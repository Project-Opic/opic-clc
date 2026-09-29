package api

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	sharedapi "github.com/sharptoolbox/opic-techbase/services/shared/pkg/sharedapi"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPermissionDiagnosticRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open sqlmock: %v", err)
	}
	defer sqlDB.Close()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}

	router := gin.New()
	SetupRoutesWithDeps(router, sharedapi.Dependencies{DB: db})
	wanted := map[string]bool{
		"GET /api/v1/permissions/diagnose/options": false,
		"POST /api/v1/permissions/diagnose":        false,
	}
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := wanted[key]; ok {
			wanted[key] = true
		}
	}
	for route, registered := range wanted {
		if !registered {
			t.Fatalf("%s is not registered", route)
		}
	}
}
