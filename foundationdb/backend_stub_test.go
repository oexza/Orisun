//go:build !foundationdb

package foundationdb

import (
	"context"
	"strings"
	"testing"

	config "github.com/OrisunLabs/Orisun/config"
)

func TestInitializeFoundationDBStubRequiresBuildTag(t *testing.T) {
	_, _, _, _, _, _, _, err := InitializeFoundationDB(
		context.Background(),
		config.FoundationDBConfig{},
		config.AdminConfig{},
		nil,
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("expected stub error")
	}
	if !strings.Contains(err.Error(), "-tags foundationdb") {
		t.Fatalf("expected build tag guidance, got %v", err)
	}
}
