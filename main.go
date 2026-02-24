package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	// Подключение к БД
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "host=postgres user=postgres password=postgres dbname=organization port=5432 sslmode=disable TimeZone=UTC"
	}
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatal("failed to connect to database:", err)
	}
	defer sqlDB.Close()

	// Миграции
	if err := goose.Up(sqlDB, "migrations"); err != nil {
		log.Fatal("failed to run migrations:", err)
	}

	// GORM
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		log.Fatal("failed to initialize gorm:", err)
	}

	// Роутер
	mux := http.NewServeMux()
	mux.HandleFunc("POST /departments/", createDepartment(gormDB))
	mux.HandleFunc("POST /departments/{id}/employees/", createEmployee(gormDB))
	mux.HandleFunc("GET /departments/{id}", getDepartment(gormDB))
	mux.HandleFunc("PATCH /departments/{id}", updateDepartment(gormDB))
	mux.HandleFunc("DELETE /departments/{id}", deleteDepartment(gormDB))

	// Запуск сервера
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	log.Printf("server starting on port %s", port)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}