package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	err = db.AutoMigrate(&Department{}, &Employee{})
	assert.NoError(t, err)
	return db
}

func TestCreateDepartment(t *testing.T) {
	db := setupTestDB(t)
	handler := createDepartment(db)

	body := CreateDepartmentRequest{Name: "IT"}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/departments/", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var resp Department
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, "IT", resp.Name)
}