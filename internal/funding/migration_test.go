package funding

import (
	"context"
	"testing"

	paymentmodel "gorixpay/internal/payment/model"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/migrate"
	"gorixpay/migrations"
)

// TestMigrationsProvidePaymentOrderKind guards the schema contract this package
// depends on: every funding order needs payment_orders.kind, created by
// migrations/000013_funding.sql, and existing rows must default to 'sale'.
func TestMigrationsProvidePaymentOrderKind(t *testing.T) {
	ctx := context.Background()
	db, _, err := database.Open(ctx, config.DatabaseConfig{Driver: "sqlite", DSN: "file:funding-migrations?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := (migrate.Runner{DB: db, FS: migrations.Files}).Up(ctx); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasColumn(&paymentmodel.Order{}, "kind") {
		t.Fatal("payment_orders.kind is missing from the migrated schema")
	}
	if !db.Migrator().HasIndex(&paymentmodel.Order{}, "idx_payment_orders_kind") {
		t.Fatal("payment_orders kind index is missing from the migrated schema")
	}
}
