package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/identity/password"
	"gorixpay/internal/platform/migrate"
	"gorixpay/migrations"
)

func main() {
	db, err := gorm.Open(sqlite.Open("gorixpay.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx := context.Background()
	if err := (migrate.Runner{DB: db, FS: migrations.Files}).Up(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
	hash, err := password.Hash("admin123")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	now := time.Now().UTC()
	admin := identitymodel.Administrator{
		ID:           uuid.NewString(),
		Username:     "admin",
		PasswordHash: hash,
		DisplayName:  "Administrator",
		Status:       identitymodel.StatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := db.WithContext(ctx).Create(&admin).Error; err != nil {
		fmt.Fprintln(os.Stderr, "create:", err)
		os.Exit(1)
	}
	fmt.Printf("Admin created: %s\nUsername: admin\nPassword: admin123\n", admin.ID)
}
