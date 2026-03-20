package config

import (
	"fmt"
	"log"
	"os"
	"strings"

	gormSQLite "github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"project-api/models"
)

var DB *gorm.DB

func ConnectDatabase() {
	logLevel := logger.Silent
	if os.Getenv("GIN_MODE") != "release" {
		logLevel = logger.Warn
	}

	gormCfg := &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
	}

	var err error
	driver := strings.ToLower(strings.TrimSpace(os.Getenv("DB_DRIVER")))

	switch driver {
	case "mysql":
		DB, err = gorm.Open(mysql.Open(mysqlDSN()), gormCfg)
	default:
		driver = "sqlite"
		DB, err = gorm.Open(gormSQLite.Open(sqliteDSN()), gormCfg)
	}

	if err != nil {
		log.Fatalf("[DB] failed to connect (%s): %v", driver, err)
	}

	if driver == "mysql" {
		sqlDB, _ := DB.DB()
		sqlDB.SetMaxOpenConns(25)
		sqlDB.SetMaxIdleConns(10)
	}

	migrate()
	seed()

	log.Printf("[DB] connected via %s", driver)
}

// ─── DSN builders ─────────────────────────────────────────────────────────────

func sqliteDSN() string {
	if dsn := os.Getenv("DATABASE_DSN"); dsn != "" {
		return dsn
	}
	return "marketplace.db"
}

func mysqlDSN() string {
	if full := os.Getenv("DATABASE_DSN"); full != "" {
		return full
	}

	host   := envOr("DB_HOST",   "127.0.0.1")
	port   := envOr("DB_PORT",   "3306")
	user   := envOr("DB_USER",   "root")
	pass   := os.Getenv("DB_PASSWORD")
	dbname := envOr("DB_NAME",   "marketplace")
	params := envOr("DB_PARAMS", "charset=utf8mb4&parseTime=True&loc=Local")

	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?%s", user, pass, host, port, dbname, params)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ─── Migration ────────────────────────────────────────────────────────────────

func migrate() {
	err := DB.AutoMigrate(
		&models.Permission{},
		&models.Role{},
		&models.User{},
		&models.Category{},
		&models.SubCategory{},
		&models.Post{},
		&models.PostLike{},  
		&models.Banner{},      // ← add
	)
	if err != nil {
		log.Fatalf("[DB] migration failed: %v", err)
	}
}

// ─── Seeding ──────────────────────────────────────────────────────────────────

func seed() {
	seedPermissions()
	administratorRole, adminRole, clientRole := seedRoles()
	seedUsers(administratorRole, adminRole, clientRole)
	seedCategories()
}

func seedPermissions() {
	perms := []models.Permission{
		// Post permissions
		{Name: "create_post",       Description: "Create new listings"},
		{Name: "edit_post",         Description: "Edit own listings"},
		{Name: "delete_post",       Description: "Delete own listings"},
		{Name: "manage_posts",      Description: "Moderate all posts"},
		// User permissions
		{Name: "create_client",     Description: "Create new client accounts"},
		{Name: "manage_users",      Description: "Create, update, and disable users"},
		// System permissions
		{Name: "manage_categories", Description: "CRUD on categories"},
		{Name: "manage_roles",      Description: "CRUD on roles and permissions"},
		{Name: "view_reports",      Description: "Access analytics dashboard"},
	}
	for _, p := range perms {
		DB.Where(models.Permission{Name: p.Name}).FirstOrCreate(&p)
	}
}

func seedRoles() (administrator models.Role, admin models.Role, client models.Role) {

	// ── Administrator ─────────────────────────────────────────────────────────
	// Single super-user account. Receives ALL permissions automatically.
	// Only one user should ever have this role.
	DB.Where(models.Role{Name: "administrator"}).FirstOrCreate(&administrator)
	DB.Model(&administrator).Update("description", "Super administrator — full system access, single user only")

	var allPerms []models.Permission
	DB.Find(&allPerms)
	if err := DB.Model(&administrator).Association("Permissions").Replace(allPerms); err != nil {
		log.Printf("[seed] administrator permissions: %v", err)
	}

	// ── Admin ─────────────────────────────────────────────────────────────────
	// Staff role. Can create clients and manage posts only.
	// Cannot access categories, roles, or staff management.
	DB.Where(models.Role{Name: "admin"}).FirstOrCreate(&admin)
	DB.Model(&admin).Update("description", "Staff admin — can create clients and manage posts")

	var adminPerms []models.Permission
	DB.Where("name IN ?", []string{
		"create_client",
		"create_post",
		"edit_post",
		"delete_post",
		"manage_posts",
		"view_reports",
	}).Find(&adminPerms)
	if err := DB.Model(&admin).Association("Permissions").Replace(adminPerms); err != nil {
		log.Printf("[seed] admin permissions: %v", err)
	}

	// ── Client ────────────────────────────────────────────────────────────────
	// Standard marketplace seller. Can only manage their own posts.
	DB.Where(models.Role{Name: "client"}).FirstOrCreate(&client)
	DB.Model(&client).Update("description", "Standard marketplace seller")

	var clientPerms []models.Permission
	DB.Where("name IN ?", []string{
		"create_post",
		"edit_post",
		"delete_post",
	}).Find(&clientPerms)
	if err := DB.Model(&client).Association("Permissions").Replace(clientPerms); err != nil {
		log.Printf("[seed] client permissions: %v", err)
	}

	return administrator, admin, client
}

func seedUsers(administratorRole, adminRole, clientRole models.Role) {

	// ── Super Administrator (one and only) ────────────────────────────────────
	var superAdmin models.User
	if DB.Where("email = ?", "admin@market.com").First(&superAdmin).Error != nil {
		hash, _ := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
		DB.Create(&models.User{
			Name:      "Administrator",
			Email:     "admin@market.com",
			Password:  string(hash),
			RoleID:    administratorRole.ID,
			PostLimit: 9999,
			IsActive:  true,
		})
	} else {
		// Migrate existing admin → administrator role on first run
		if superAdmin.RoleID != administratorRole.ID {
			DB.Model(&superAdmin).Update("role_id", administratorRole.ID)
			log.Printf("[seed] migrated admin@market.com → administrator role")
		}
	}

	// ── Demo Staff Admin ──────────────────────────────────────────────────────
	var staffAdmin models.User
	if DB.Where("email = ?", "staff@market.com").First(&staffAdmin).Error != nil {
		hash, _ := bcrypt.GenerateFromPassword([]byte("staff123"), bcrypt.DefaultCost)
		DB.Create(&models.User{
			Name:      "Staff Admin",
			Email:     "staff@market.com",
			Password:  string(hash),
			RoleID:    adminRole.ID,
			PostLimit: 9999,
			IsActive:  true,
		})
	}

	// ── Demo Client ───────────────────────────────────────────────────────────
	var demoUser models.User
	if DB.Where("email = ?", "john@example.com").First(&demoUser).Error != nil {
		hash, _ := bcrypt.GenerateFromPassword([]byte("client123"), bcrypt.DefaultCost)
		DB.Create(&models.User{
			Name:      "John Doe",
			Email:     "john@example.com",
			Password:  string(hash),
			RoleID:    clientRole.ID,
			PostLimit: 10,
			IsActive:  true,
		})
	}
}

func seedCategories() {
	cats := []models.Category{
		{Name: "Electronics", Description: "Phones, laptops, gadgets",   Image: ""},
		{Name: "Vehicles",    Description: "Cars, motorcycles, boats",    Image: ""},
		{Name: "Real Estate", Description: "Houses, apartments, land",    Image: ""},
		{Name: "Fashion",     Description: "Clothes, shoes, accessories", Image: ""},
		{Name: "Furniture",   Description: "Home and office furniture",   Image: ""},
		{Name: "Jobs",        Description: "Employment opportunities",    Image: ""},
		{Name: "Sports",      Description: "Sports equipment & fitness",  Image: ""},
		{Name: "Books",       Description: "Books, textbooks, magazines", Image: ""},
	}
	for _, c := range cats {
		DB.Where(models.Category{Name: c.Name}).FirstOrCreate(&c)
	}
}