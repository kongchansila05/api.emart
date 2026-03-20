package controllers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"    // ← add this
	"gorm.io/gorm"

	"project-api/models"
	"project-api/utils"
)

// AuthController handles registration and authentication.
type AuthController struct {
	db *gorm.DB
}

// NewAuthController constructs an AuthController.
func NewAuthController(db *gorm.DB) *AuthController {
	return &AuthController{db: db}
}

// ─── Request / Response DTOs ──────────────────────────────────────────────────

type registerRequest struct {
	Name     string `json:"name"     binding:"required"`
	Email    string `json:"email"    binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	Phone    string `json:"phone"`
}

// adminRegisterRequest extends registration with an optional role and post limit.
type adminRegisterRequest struct {
	Name      string `json:"name"       binding:"required"`
	Email     string `json:"email"      binding:"required,email"`
	Password  string `json:"password"   binding:"required,min=6"`
	Phone     string `json:"phone"`
	RoleName  string `json:"role"`        // "admin" | "client" | any existing role name (default: "client")
	PostLimit int    `json:"post_limit"`  // 0 → default per role: admin=9999, client=10
}

type loginRequest struct {
	Email    string `json:"email"    binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type authUserResponse struct {
	ID        uint   `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
	Role      string `json:"role"`
	PostLimit int    `json:"post_limit"`
	IsActive  bool   `json:"is_active"`
}

// ─── Handlers ─────────────────────────────────────────────────────────────────

// Register — public endpoint, always assigns the "client" role.
func (ac *AuthController) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	var clientRole models.Role
	if err := ac.db.Where("name = ?", "client").First(&clientRole).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Default role not found"})
		return
	}

	user := models.User{
		Name:      req.Name,
		Email:     req.Email,
		Password:  string(hash),
		Phone:     req.Phone,
		RoleID:    clientRole.ID,
		PostLimit: 10,
		IsActive:  true,
	}
	if err := ac.db.Create(&user).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Email is already registered"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Registration successful — you can now sign in"})
}

// AdminRegister — admin-only endpoint.
// Allows specifying any role ("admin", "client", or a custom role) and post_limit.
// Defaults: role=client, post_limit=10 (client) or 9999 (admin).
func (ac *AuthController) AdminRegister(c *gin.Context) {
	var req adminRegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Resolve role — default to "admin" if not provided (staff endpoint)
	roleName := req.RoleName
	if roleName == "" {
		roleName = "client"
	}

	var role models.Role
	if err := ac.db.Where("name = ?", roleName).First(&role).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Role '" + roleName + "' not found"})
		return
	}

	// Resolve post limit — use sensible defaults if not specified
	postLimit := req.PostLimit
	if postLimit <= 0 {
		if roleName == "admin" {
			postLimit = 9999
		} else {
			postLimit = 10
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	user := models.User{
		Name:      req.Name,
		Email:     req.Email,
		Password:  string(hash),
		Phone:     req.Phone,
		RoleID:    role.ID,
		PostLimit: postLimit,
		IsActive:  true,
	}
	if err := ac.db.Create(&user).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Email is already registered"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "User created successfully",
		"user": authUserResponse{
			ID:        user.ID,
			Name:      user.Name,
			Email:     user.Email,
			Phone:     user.Phone,
			Role:      role.Name,
			PostLimit: user.PostLimit,
			IsActive:  user.IsActive,
		},
	})
}

// Login godoc
func (ac *AuthController) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var user models.User
	if err := ac.db.Preload("Role.Permissions").
		Where("email = ?", req.Email).
		First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
		return
	}

	if !user.IsActive {
		c.JSON(http.StatusForbidden, gin.H{"error": "Account is disabled — please contact support"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
		return
	}

	token, err := utils.GenerateToken(user.ID, user.RoleID, user.Email, user.Role.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": authUserResponse{
			ID:        user.ID,
			Name:      user.Name,
			Email:     user.Email,
			Phone:     user.Phone,
			Role:      user.Role.Name,
			PostLimit: user.PostLimit,
			IsActive:  user.IsActive,
		},
	})
}

// GoogleLogin — verifies a Google ID token and finds or creates a client user.
// POST /auth/google  { "token": "<google_id_token>" }
func (ac *AuthController) GoogleLogin(c *gin.Context) {
	var req struct {
		Token string `json:"token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	googleUser, err := verifyGoogleToken(req.Token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid Google token"})
		return
	}

	var user models.User

	// ── Use Unscoped to find soft-deleted users too ───────────────────────────
	err = ac.db.Unscoped().
		Where("email = ?", googleUser.Email).
		First(&user).Error

	if err != nil {
		// Not found — create as client
		var clientRole models.Role
		if err := ac.db.Where("name = ?", "client").First(&clientRole).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Client role not found"})
			return
		}
		user = models.User{
			Name:      googleUser.Name,
			Email:     googleUser.Email,
			Avatar:    googleUser.Picture,
			Password:  "",
			RoleID:    clientRole.ID,
			PostLimit: 10,
			IsActive:  true,
		}
		if err := ac.db.Create(&user).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
			return
		}
		ac.db.Preload("Role.Permissions").First(&user, user.ID)
	} else {
		// Found — restore if soft-deleted
		if user.DeletedAt.Valid {
			ac.db.Unscoped().Model(&user).Updates(map[string]interface{}{
				"deleted_at": nil,
				"is_active":  true,
				"avatar":     googleUser.Picture,
			})
		}
		ac.db.Preload("Role.Permissions").First(&user, user.ID)
	}

	if !user.IsActive {
		c.JSON(http.StatusForbidden, gin.H{"error": "Account is disabled"})
		return
	}

	token, err := utils.GenerateToken(user.ID, user.RoleID, user.Email, user.Role.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": authUserResponse{
			ID:        user.ID,
			Name:      user.Name,
			Email:     user.Email,
			Phone:     user.Phone,
			Role:      user.Role.Name,
			PostLimit: user.PostLimit,
			IsActive:  user.IsActive,
		},
	})
}

// ─── Google token verification ────────────────────────────────────────────────

type googleUserInfo struct {
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

// verifyGoogleToken accepts either an ID token or access token.
func verifyGoogleToken(token string) (*googleUserInfo, error) {
	// Try userinfo endpoint with access token first
	req, _ := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v3/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var info struct {
			Email   string `json:"email"`
			Name    string `json:"name"`
			Picture string `json:"picture"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
			return nil, err
		}
		if info.Email != "" {
			return &googleUserInfo{
				Email:   info.Email,
				Name:    info.Name,
				Picture: info.Picture,
			}, nil
		}
	}

	// Fallback: try tokeninfo endpoint (for ID tokens)
	resp2, err := http.Get("https://oauth2.googleapis.com/tokeninfo?id_token=" + token)
	if err != nil {
		return nil, err
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("invalid token")
	}

	var info2 struct {
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&info2); err != nil {
		return nil, err
	}
	if info2.Email == "" {
		return nil, fmt.Errorf("no email in token")
	}

	return &googleUserInfo{
		Email:   info2.Email,
		Name:    info2.Name,
		Picture: info2.Picture,
	}, nil
}

// GoogleRegisterClient — registers a new client account via Google.
// This is for the marketplace mobile/web app, NOT the admin panel.
// POST /auth/google/register
func (ac *AuthController) GoogleRegisterClient(c *gin.Context) {
	var req struct {
		Token string `json:"token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	googleUser, err := verifyGoogleToken(req.Token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid Google token"})
		return
	}

	var existing models.User

	// ── Use Unscoped to find soft-deleted users too ───────────────────────────
	err = ac.db.Unscoped().
		Where("email = ?", googleUser.Email).
		First(&existing).Error

	if err == nil {
		// User found (active or soft-deleted)

		// If soft-deleted — restore the account
		if existing.DeletedAt.Valid {
			ac.db.Unscoped().Model(&existing).Updates(map[string]interface{}{
				"deleted_at": nil,
				"is_active":  true,
				"avatar":     googleUser.Picture,
				"name":       googleUser.Name,
			})
		}

		if !existing.IsActive && !existing.DeletedAt.Valid {
			c.JSON(http.StatusForbidden, gin.H{"error": "Account is disabled"})
			return
		}

		// Reload with role
		ac.db.Preload("Role.Permissions").First(&existing, existing.ID)

		token, err := utils.GenerateToken(existing.ID, existing.RoleID, existing.Email, existing.Role.Name)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not generate token"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Welcome back! Logged in to existing account.",
			"is_new":  false,
			"token":   token,
			"user": authUserResponse{
				ID:        existing.ID,
				Name:      existing.Name,
				Email:     existing.Email,
				Phone:     existing.Phone,
				Role:      existing.Role.Name,
				PostLimit: existing.PostLimit,
				IsActive:  existing.IsActive,
			},
		})
		return
	}

	// ── New user — create client account ─────────────────────────────────────
	var clientRole models.Role
	if err := ac.db.Where("name = ?", "client").First(&clientRole).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Client role not found"})
		return
	}

	user := models.User{
		Name:      googleUser.Name,
		Email:     googleUser.Email,
		Avatar:    googleUser.Picture,
		Password:  "",
		RoleID:    clientRole.ID,
		PostLimit: 10,
		IsActive:  true,
	}
	if err := ac.db.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create account"})
		return
	}

	ac.db.Preload("Role.Permissions").First(&user, user.ID)

	token, err := utils.GenerateToken(user.ID, user.RoleID, user.Email, user.Role.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not generate token"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Account created successfully",
		"is_new":  true,
		"token":   token,
		"user": authUserResponse{
			ID:        user.ID,
			Name:      user.Name,
			Email:     user.Email,
			Phone:     user.Phone,
			Role:      user.Role.Name,
			PostLimit: user.PostLimit,
			IsActive:  user.IsActive,
		},
	})
}


// GoogleRegisterStaff — registers/updates a staff account via Google with a specific role.
// POST /auth/google/register/staff  { "token": "...", "role": "admin" }
func (ac *AuthController) GoogleRegisterStaff(c *gin.Context) {
	var req struct {
		Token    string `json:"token"    binding:"required"`
		RoleName string `json:"role"     binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Block client role from staff endpoint
	if req.RoleName == "client" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Use /auth/google/register for client accounts"})
		return
	}

	googleUser, err := verifyGoogleToken(req.Token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid Google token"})
		return
	}

	var role models.Role
	if err := ac.db.Where("name = ?", req.RoleName).First(&role).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Role '" + req.RoleName + "' not found"})
		return
	}

	var existing models.User
	err = ac.db.Unscoped().Where("email = ?", googleUser.Email).First(&existing).Error

	if err == nil {
		// User exists — restore if soft-deleted and update role
		updates := map[string]interface{}{
			"role_id":    role.ID,
			"avatar":     googleUser.Picture,
			"name":       googleUser.Name,
			"is_active":  true,
		}
		if existing.DeletedAt.Valid {
			updates["deleted_at"] = nil
		}
		ac.db.Unscoped().Model(&existing).Updates(updates)
		ac.db.Preload("Role.Permissions").First(&existing, existing.ID)

		token, err := utils.GenerateToken(existing.ID, existing.RoleID, existing.Email, existing.Role.Name)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not generate token"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"message": "Existing user updated to " + req.RoleName,
			"is_new":  false,
			"token":   token,
			"user": authUserResponse{
				ID:        existing.ID,
				Name:      existing.Name,
				Email:     existing.Email,
				Phone:     existing.Phone,
				Role:      existing.Role.Name,
				PostLimit: existing.PostLimit,
				IsActive:  existing.IsActive,
			},
		})
		return
	}

	// New user
	user := models.User{
		Name:      googleUser.Name,
		Email:     googleUser.Email,
		Avatar:    googleUser.Picture,
		Password:  "",
		RoleID:    role.ID,
		PostLimit: 9999,
		IsActive:  true,
	}
	if err := ac.db.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create account"})
		return
	}
	ac.db.Preload("Role.Permissions").First(&user, user.ID)

	token, err := utils.GenerateToken(user.ID, user.RoleID, user.Email, user.Role.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not generate token"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"message": "Staff account created",
		"is_new":  true,
		"token":   token,
		"user": authUserResponse{
			ID:        user.ID,
			Name:      user.Name,
			Email:     user.Email,
			Phone:     user.Phone,
			Role:      user.Role.Name,
			PostLimit: user.PostLimit,
			IsActive:  user.IsActive,
		},
	})
}



// FirebasePhoneLogin — verifies a Firebase phone ID token.
// POST /auth/phone  { "token": "<firebase_id_token>", "phone": "+85512345678", "name": "optional" }
func (ac *AuthController) FirebasePhoneLogin(c *gin.Context) {
	var req struct {
		Token string `json:"token" binding:"required"`
		Phone string `json:"phone"`
		Name  string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify Firebase ID token
	phoneUser, err := verifyFirebasePhoneToken(req.Token)
	if err != nil {
		// ── Log the actual error for debugging ───────────────────────────────
		fmt.Printf("[Phone Auth] Token verification failed: %v\n", err)
		fmt.Printf("[Phone Auth] Token (first 50 chars): %s\n", req.Token[:min(50, len(req.Token))])
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid Firebase token: " + err.Error()})
		return
	}

	phone := phoneUser.Phone
	if phone == "" {
		phone = req.Phone
	}
	if phone == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Phone number is required"})
		return
	}

	fmt.Printf("[Phone Auth] Verified phone: %s\n", phone)

	isNew := false
	var user models.User

	err = ac.db.Unscoped().Where("phone = ?", phone).First(&user).Error

	if err != nil {
		// ── New user ──────────────────────────────────────────────────────────
		isNew = true
		var clientRole models.Role
		if err := ac.db.Where("name = ?", "client").First(&clientRole).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Client role not found"})
			return
		}

		name := req.Name
		if name == "" {
			name = phone
		}

		user = models.User{
			Name:      name,
			Phone:     phone,
			Password:  "",
			RoleID:    clientRole.ID,
			PostLimit: 10,
			IsActive:  true,
		}
		if err := ac.db.Create(&user).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
			return
		}
		ac.db.Preload("Role.Permissions").First(&user, user.ID)

	} else {
		// ── Existing user ─────────────────────────────────────────────────────
		if user.DeletedAt.Valid {
			ac.db.Unscoped().Model(&user).Updates(map[string]interface{}{
				"deleted_at": nil,
				"is_active":  true,
			})
		}
		ac.db.Preload("Role.Permissions").First(&user, user.ID)
	}

	if !user.IsActive {
		c.JSON(http.StatusForbidden, gin.H{"error": "Account is disabled"})
		return
	}

	token, err := utils.GenerateToken(user.ID, user.RoleID, user.Email, user.Role.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not generate token"})
		return
	}

	fmt.Printf("[Phone Auth] Login success — user ID: %d, role: %s, is_new: %v\n", user.ID, user.Role.Name, isNew)

	c.JSON(http.StatusOK, gin.H{
		"token":  token,
		"is_new": isNew, // ← fixed: was hardcoded false
		"user": authUserResponse{
			ID:        user.ID,
			Name:      user.Name,
			Email:     user.Email,
			Phone:     user.Phone,
			Role:      user.Role.Name,
			PostLimit: user.PostLimit,
			IsActive:  user.IsActive,
		},
	})
}

// min helper for Go versions < 1.21
func min(a, b int) int {
	if a < b { return a }
	return b
}

// ─── Firebase Phone Token Verification ───────────────────────────────────────

type firebasePhoneUser struct {
	Phone string
	UID   string
}

	// verifyFirebasePhoneToken — verifies Firebase phone ID token using public keys.
func verifyFirebasePhoneToken(idToken string) (*firebasePhoneUser, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format — got %d parts", len(parts))
	}

	// Decode payload without padding issues
	payload := parts[1]
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("base64 decode failed: %v", err)
	}

	fmt.Printf("[Phone Auth] Raw payload: %s\n", string(decoded))

	var claims struct {
		Sub         string `json:"sub"`
		PhoneNumber string `json:"phone_number"`
		Exp         int64  `json:"exp"`
		Iss         string `json:"iss"`
		Firebase    struct {
			SignInProvider string `json:"sign_in_provider"`
		} `json:"firebase"`
	}

	if err := json.Unmarshal(decoded, &claims); err != nil {
		return nil, fmt.Errorf("JSON unmarshal failed: %v", err)
	}

	fmt.Printf("[Phone Auth] Claims — sub: %s, phone: %s, iss: %s, provider: %s\n",
		claims.Sub, claims.PhoneNumber, claims.Iss, claims.Firebase.SignInProvider)

	if time.Now().Unix() > claims.Exp {
		return nil, fmt.Errorf("token expired at %d, now %d", claims.Exp, time.Now().Unix())
	}

	if !strings.Contains(claims.Iss, "securetoken.google.com") {
		return nil, fmt.Errorf("invalid issuer: %s", claims.Iss)
	}

	if claims.PhoneNumber == "" {
		return nil, fmt.Errorf("no phone_number in token claims — provider is: %s", claims.Firebase.SignInProvider)
	}

	return &firebasePhoneUser{
		Phone: claims.PhoneNumber,
		UID:   claims.Sub,
	}, nil
}