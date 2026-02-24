package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"gorm.io/gorm"
)

// Request/Response структуры
type CreateDepartmentRequest struct {
	Name     string `json:"name"`
	ParentID *int   `json:"parent_id"`
}

type UpdateDepartmentRequest struct {
	Name     *string `json:"name"`
	ParentID *int    `json:"parent_id"`
}

type CreateEmployeeRequest struct {
	FullName string     `json:"full_name"`
	Position string     `json:"position"`
	HiredAt  *time.Time `json:"hired_at"`
}

type DepartmentResponse struct {
	ID        int                `json:"id"`
	Name      string             `json:"name"`
	ParentID  *int               `json:"parent_id"`
	CreatedAt time.Time          `json:"created_at"`
	Employees []EmployeeResponse  `json:"employees,omitempty"`
	Children  []DepartmentResponse `json:"children,omitempty"`
}

type EmployeeResponse struct {
	ID           int        `json:"id"`
	DepartmentID int        `json:"department_id"`
	FullName     string     `json:"full_name"`
	Position     string     `json:"position"`
	HiredAt      *time.Time `json:"hired_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

// Утилиты для ответов
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}

// 1) Создать подразделение
func createDepartment(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateDepartmentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.ParentID != nil {
			var parent Department
			if err := db.First(&parent, *req.ParentID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					respondError(w, http.StatusNotFound, "parent department not found")
				} else {
					respondError(w, http.StatusInternalServerError, "database error")
				}
				return
			}
		}
		dept := Department{
			Name:     req.Name,
			ParentID: req.ParentID,
		}
		if err := db.Create(&dept).Error; err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondJSON(w, http.StatusCreated, dept)
	}
}

// 2) Создать сотрудника в подразделении
func createEmployee(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		deptID, err := strconv.Atoi(idStr)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid department id")
			return
		}
		var dept Department
		if err := db.First(&dept, deptID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				respondError(w, http.StatusNotFound, "department not found")
			} else {
				respondError(w, http.StatusInternalServerError, "database error")
			}
			return
		}
		var req CreateEmployeeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		emp := Employee{
			DepartmentID: deptID,
			FullName:     req.FullName,
			Position:     req.Position,
			HiredAt:      req.HiredAt,
		}
		if err := db.Create(&emp).Error; err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondJSON(w, http.StatusCreated, emp)
	}
}

// 3) Получить подразделение (детали + сотрудники + поддерево)
func getDepartment(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		deptID, err := strconv.Atoi(idStr)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid department id")
			return
		}

		// Парсинг query-параметров
		depth := 1
		if d := r.URL.Query().Get("depth"); d != "" {
			if val, err := strconv.Atoi(d); err == nil && val >= 1 && val <= 5 {
				depth = val
			} else {
				respondError(w, http.StatusBadRequest, "depth must be integer between 1 and 5")
				return
			}
		}
		includeEmployees := true
		if ie := r.URL.Query().Get("include_employees"); ie != "" {
			if val, err := strconv.ParseBool(ie); err == nil {
				includeEmployees = val
			} else {
				respondError(w, http.StatusBadRequest, "include_employees must be boolean")
				return
			}
		}
		sortBy := r.URL.Query().Get("sort_employees")
		if sortBy == "" {
			sortBy = "full_name"
		} else if sortBy != "full_name" && sortBy != "created_at" {
			respondError(w, http.StatusBadRequest, "sort_employees must be 'full_name' or 'created_at'")
			return
		}

		// Загружаем корневой отдел
		var rootDept Department
		if err := db.First(&rootDept, deptID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				respondError(w, http.StatusNotFound, "department not found")
			} else {
				respondError(w, http.StatusInternalServerError, "database error")
			}
			return
		}

		// Строим дерево в памяти (максимум depth уровней)
		type deptNode struct {
			Department
			Level int
		}
		deptMap := map[int]*deptNode{rootDept.ID: {Department: rootDept, Level: 0}}
		currentIDs := []int{rootDept.ID}

		for level := 1; level <= depth; level++ {
			if len(currentIDs) == 0 {
				break
			}
			var children []Department
			if err := db.Where("parent_id IN ?", currentIDs).Find(&children).Error; err != nil {
				respondError(w, http.StatusInternalServerError, "database error")
				return
			}
			nextIDs := make([]int, 0, len(children))
			for _, child := range children {
				deptMap[child.ID] = &deptNode{Department: child, Level: level}
				nextIDs = append(nextIDs, child.ID)
			}
			currentIDs = nextIDs
		}

		// Связываем родителей и детей
		for _, node := range deptMap {
			if node.ParentID != nil {
				if parent, ok := deptMap[*node.ParentID]; ok {
					parent.Children = append(parent.Children, node.Department)
				}
			}
		}

		// Загружаем сотрудников, если нужно
		if includeEmployees {
			deptIDs := make([]int, 0, len(deptMap))
			for id := range deptMap {
				deptIDs = append(deptIDs, id)
			}
			var employees []Employee
			query := db.Where("department_id IN ?", deptIDs)
			if sortBy == "full_name" {
				query = query.Order("full_name")
			} else {
				query = query.Order("created_at")
			}
			if err := query.Find(&employees).Error; err != nil {
				respondError(w, http.StatusInternalServerError, "database error")
				return
			}
			empMap := make(map[int][]Employee)
			for _, emp := range employees {
				empMap[emp.DepartmentID] = append(empMap[emp.DepartmentID], emp)
			}
			for _, node := range deptMap {
				node.Employees = empMap[node.ID]
			}
		}

		// Рекурсивное преобразование в ответ
		var toResponse func(dept *Department, currentDepth int) DepartmentResponse
		toResponse = func(dept *Department, currentDepth int) DepartmentResponse {
			resp := DepartmentResponse{
				ID:        dept.ID,
				Name:      dept.Name,
				ParentID:  dept.ParentID,
				CreatedAt: dept.CreatedAt,
			}
			if includeEmployees {
				for _, emp := range dept.Employees {
					resp.Employees = append(resp.Employees, EmployeeResponse{
						ID:           emp.ID,
						DepartmentID: emp.DepartmentID,
						FullName:     emp.FullName,
						Position:     emp.Position,
						HiredAt:      emp.HiredAt,
						CreatedAt:    emp.CreatedAt,
					})
				}
			}
			if currentDepth < depth {
				for _, child := range dept.Children {
					resp.Children = append(resp.Children, toResponse(&child, currentDepth+1))
				}
			}
			return resp
		}

		respondJSON(w, http.StatusOK, toResponse(&rootDept, 0))
	}
}

// 4) Переместить подразделение (обновить)
func updateDepartment(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		deptID, err := strconv.Atoi(idStr)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid department id")
			return
		}
		var req UpdateDepartmentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		var dept Department
		if err := db.First(&dept, deptID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				respondError(w, http.StatusNotFound, "department not found")
			} else {
				respondError(w, http.StatusInternalServerError, "database error")
			}
			return
		}
		if req.ParentID != nil {
			// Проверяем существование нового родителя
			if *req.ParentID != 0 {
				var parent Department
				if err := db.First(&parent, *req.ParentID).Error; err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						respondError(w, http.StatusNotFound, "parent department not found")
					} else {
						respondError(w, http.StatusInternalServerError, "database error")
					}
					return
				}
			}
			// Проверка цикла
			if err := dept.CheckCycle(db, req.ParentID); err != nil {
				respondError(w, http.StatusConflict, err.Error())
				return
			}
			dept.ParentID = req.ParentID
		}
		if req.Name != nil {
			dept.Name = *req.Name
		}
		if err := db.Save(&dept).Error; err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondJSON(w, http.StatusOK, dept)
	}
}

// 5) Удалить подразделение
func deleteDepartment(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		deptID, err := strconv.Atoi(idStr)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid department id")
			return
		}
		mode := r.URL.Query().Get("mode")
		if mode == "" {
			mode = "cascade" // значение по умолчанию
		}
		reassignToStr := r.URL.Query().Get("reassign_to_department_id")
		var reassignTo *int
		if reassignToStr != "" {
			val, err := strconv.Atoi(reassignToStr)
			if err != nil {
				respondError(w, http.StatusBadRequest, "invalid reassign_to_department_id")
				return
			}
			reassignTo = &val
		}
		if mode == "reassign" && reassignTo == nil {
			respondError(w, http.StatusBadRequest, "reassign_to_department_id is required for reassign mode")
			return
		}
		var dept Department
		if err := db.First(&dept, deptID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				respondError(w, http.StatusNotFound, "department not found")
			} else {
				respondError(w, http.StatusInternalServerError, "database error")
			}
			return
		}
		// Транзакция
		tx := db.Begin()
		defer func() {
			if r := recover(); r != nil {
				tx.Rollback()
			}
		}()
		if err := tx.Error; err != nil {
			respondError(w, http.StatusInternalServerError, "transaction error")
			return
		}
		if mode == "cascade" {
			// Каскадное удаление через внешние ключи (ON DELETE CASCADE)
			if err := tx.Delete(&dept).Error; err != nil {
				tx.Rollback()
				respondError(w, http.StatusInternalServerError, err.Error())
				return
			}
		} else if mode == "reassign" {
			// Проверяем существование целевого отдела
			var targetDept Department
			if err := tx.First(&targetDept, *reassignTo).Error; err != nil {
				tx.Rollback()
				if errors.Is(err, gorm.ErrRecordNotFound) {
					respondError(w, http.StatusNotFound, "reassign target department not found")
				} else {
					respondError(w, http.StatusInternalServerError, "database error")
				}
				return
			}
			// Переназначаем сотрудников
			if err := tx.Model(&Employee{}).Where("department_id = ?", deptID).Update("department_id", *reassignTo).Error; err != nil {
				tx.Rollback()
				respondError(w, http.StatusInternalServerError, err.Error())
				return
			}
			// Переназначаем дочерние подразделения
			if err := tx.Model(&Department{}).Where("parent_id = ?", deptID).Update("parent_id", *reassignTo).Error; err != nil {
				tx.Rollback()
				respondError(w, http.StatusInternalServerError, err.Error())
				return
			}
			// Удаляем сам отдел
			if err := tx.Delete(&dept).Error; err != nil {
				tx.Rollback()
				respondError(w, http.StatusInternalServerError, err.Error())
				return
			}
		} else {
			respondError(w, http.StatusBadRequest, "mode must be 'cascade' or 'reassign'")
			return
		}
		if err := tx.Commit().Error; err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}