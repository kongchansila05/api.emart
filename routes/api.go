package routes

import (
	"log"
	"os"

	"github.com/gin-gonic/gin"

	"project-api/config"
	"project-api/controllers"
	"project-api/middleware"
	"project-api/services"
)

// Register wires every controller and attaches all routes to the engine.
func Register(r *gin.Engine) {
	db := config.DB

	// ── Shared services ──────────────────────────────────────────────────────
	postSvc := services.NewPostService(db)

	r2, err := services.NewR2Service(
		os.Getenv("R2_ACCOUNT_ID"),
		os.Getenv("R2_ACCESS_KEY"),
		os.Getenv("R2_SECRET_KEY"),
		os.Getenv("R2_BUCKET"),
		os.Getenv("R2_PUBLIC_URL"),
	)
	if err != nil {
		log.Fatalf("[R2] failed to initialise: %v", err)
	}

	// ── Controllers ──────────────────────────────────────────────────────────
	authCtrl   := controllers.NewAuthController(db)
	postCtrl   := controllers.NewPostController(postSvc)
	catCtrl    := controllers.NewCategoryController(db, r2)
	userCtrl   := controllers.NewUserController(db, postSvc)
	uploadCtrl := controllers.NewUploadController(r2)
	bannerCtrl := controllers.NewBannerController(db)   // ← add

	// ── Middleware bundles ───────────────────────────────────────────────────
	auth  := middleware.Authenticate()
	admin := middleware.RequireAdmin()

	api := r.Group("/api")

	// ════════════════════════════════════════════════════════════════════════
	// PUBLIC — No authentication required
	// ════════════════════════════════════════════════════════════════════════
	{
		api.POST("/auth/register/client", authCtrl.Register)
		api.POST("/auth/login", authCtrl.Login)
		api.POST("/auth/google",         authCtrl.GoogleLogin)   
		api.POST("/auth/google/register",  authCtrl.GoogleRegisterClient)  // ← add
		api.POST("/auth/google/register/staff", authCtrl.GoogleRegisterStaff)  // ← add

		api.POST("/auth/phone", authCtrl.FirebasePhoneLogin)


		api.GET("/posts", postCtrl.GetAll)
		api.GET("/posts/:id", postCtrl.GetOne)
		api.GET("/categories", catCtrl.GetActive)
		api.GET("/categories/:id/sub-categories",       catCtrl.GetSubCategories)
		// Public banner endpoints
		api.GET("/banners",            bannerCtrl.GetActive)      // list active banners
	}

	// ════════════════════════════════════════════════════════════════════════
	// PROTECTED — Any authenticated user
	// ════════════════════════════════════════════════════════════════════════
	protected := api.Group("/")
	protected.Use(auth)
	{
		protected.GET("/me", userCtrl.GetMe)

		protected.GET("/my-posts", postCtrl.GetMine)
		protected.POST("/posts", postCtrl.Create)
		protected.PUT("/posts/:id", postCtrl.Update)
		protected.DELETE("/posts/:id", postCtrl.Delete)
		protected.POST("/posts/:id/like", postCtrl.ToggleLike)  // ← add this

		
	}

	// ════════════════════════════════════════════════════════════════════════
	// ADMIN — Requires role = "admin"
	// ════════════════════════════════════════════════════════════════════════
	adminGroup := api.Group("/admin")
	adminGroup.Use(auth, admin)
	{
		// Dashboard
		adminGroup.GET("/stats", userCtrl.GetStats)

		// ── Auth ─────────────────────────────────────────────────────────────
		api.POST("/auth/register/staff", authCtrl.AdminRegister)

		// ── Upload ───────────────────────────────────────────────────────────
		adminGroup.POST("/upload", uploadCtrl.UploadImage)

		// ── Users ────────────────────────────────────────────────────────────
		adminGroup.GET("/users", userCtrl.AdminGetUsers)
		adminGroup.PUT("/users/:id", userCtrl.AdminUpdateUser)
		adminGroup.DELETE("/users/:id", userCtrl.AdminDeleteUser)
		adminGroup.PATCH("/users/:id/limit", userCtrl.AdminSetPostLimit)
		adminGroup.PATCH("/users/:id/image-limit", userCtrl.AdminSetImageLimit)  // ← new
		adminGroup.PATCH("/users/:id/status", userCtrl.AdminToggleStatus)
		

		// ── Categories ───────────────────────────────────────────────────────
		adminGroup.GET("/categories", catCtrl.AdminGetAll)
		adminGroup.POST("/categories", catCtrl.AdminCreate)
		adminGroup.PUT("/categories/:id", catCtrl.AdminUpdate)
		adminGroup.DELETE("/categories/:id", catCtrl.AdminDelete)
		// ── SubCategories ─────────────────────────────────────────────────────────────
		adminGroup.GET("/categories/:id/sub-categories",                     catCtrl.AdminGetSubCategories)
		adminGroup.POST("/categories/:id/sub-categories",                    catCtrl.AdminCreateSubCategory)
		adminGroup.PUT("/categories/:id/sub-categories/:sub_id",             catCtrl.AdminUpdateSubCategory)
		adminGroup.DELETE("/categories/:id/sub-categories/:sub_id",          catCtrl.AdminDeleteSubCategory)
		adminGroup.PATCH("/categories/:id/sub-categories/:sub_id/status",    catCtrl.AdminToggleSubCategoryStatus)

		// ── Roles ────────────────────────────────────────────────────────────
		adminGroup.GET("/roles", userCtrl.GetRoles)
		adminGroup.GET("/roles/staff", userCtrl.GetStaffRoles)
		adminGroup.POST("/roles", userCtrl.CreateRole)
		adminGroup.PUT("/roles/:id", userCtrl.UpdateRole)
		adminGroup.DELETE("/roles/:id", userCtrl.DeleteRole)

		// ── Permissions ──────────────────────────────────────────────────────
		adminGroup.GET("/permissions", userCtrl.GetPermissions)
		adminGroup.POST("/permissions", userCtrl.CreatePermission)
		adminGroup.DELETE("/permissions/:id", userCtrl.DeletePermission)

		// ── Posts ────────────────────────────────────────────────────────────
		adminGroup.GET("/posts", postCtrl.AdminGetAll)
		adminGroup.POST("/posts",               postCtrl.AdminCreatePost)   // ← add this
		adminGroup.PUT("/posts/:id",            postCtrl.AdminUpdatePost)   // ← add this
		adminGroup.PATCH("/posts/:id/status", postCtrl.AdminUpdateStatus)
		adminGroup.DELETE("/posts/:id", postCtrl.AdminDelete)
		
		// ── Banners ───────────────────────────────────────────────────────────
		adminGroup.GET("/banners",               bannerCtrl.AdminGetAll)
		adminGroup.POST("/banners",              bannerCtrl.AdminCreate)
		adminGroup.PUT("/banners/:id",           bannerCtrl.AdminUpdate)
		adminGroup.PATCH("/banners/:id/status",  bannerCtrl.AdminToggleActive)
		adminGroup.DELETE("/banners/:id",        bannerCtrl.AdminDelete)
	}
}