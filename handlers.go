package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
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
	Name        *string `json:"name"`
	ParentID    *int    `json:"parent_id"`
	HasName     bool    `json:"-"`
	HasParentID bool    `json:"-"`
}

func (r *UpdateDepartmentRequest) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	if v, ok := raw["name"]; ok {
		r.HasName = true
		if string(v) != "null" {
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return err
			}
			r.Name = &s
		}
	}

	if v, ok := raw["parent_id"]; ok {
		r.HasParentID = true
		if string(v) != "null" {
			var id int
			if err := json.Unmarshal(v, &id); err != nil {
				return err
			}
			r.ParentID = &id
		}
	}

	return nil
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
	_ = json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}

func parsePositiveID(raw string) (int, bool) {
	id, err := strconv.Atoi(raw)
	return id, err == nil && id > 0
}

// 1) Создать подразделение
func createDepartment(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateDepartmentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if req.ParentID != nil && *req.ParentID <= 0 {
			respondError(w, http.StatusBadRequest, "parent_id must be a positive integer")
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
		deptID, ok := parsePositiveID(idStr)
		if !ok {
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
		deptID, ok := parsePositiveID(idStr)
		if !ok {
			respondError(w, http.StatusBadRequest, "invalid department id")
			return
		}

		// query-параметры
		depth := 1
		if d := r.URL.Query().Get("depth"); d != "" {
			val, err := strconv.Atoi(d)
			if err != nil || val < 1 || val > 5 {
				respondError(w, http.StatusBadRequest, "depth must be integer between 1 and 5")
				return
			}
			depth = val
		}

		includeEmployees := true
		if ie := r.URL.Query().Get("include_employees"); ie != "" {
			val, err := strconv.ParseBool(ie)
			if err != nil {
				respondError(w, http.StatusBadRequest, "include_employees must be boolean")
				return
			}
			includeEmployees = val
		}

		sortBy := r.URL.Query().Get("sort_employees")
		if sortBy == "" {
			sortBy = "full_name"
		} else if sortBy != "full_name" && sortBy != "created_at" {
			respondError(w, http.StatusBadRequest, "sort_employees must be 'full_name' or 'created_at'")
			return
		}

		// корневой отдел
		var rootDept Department
		if err := db.First(&rootDept, deptID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				respondError(w, http.StatusNotFound, "department not found")
			} else {
				respondError(w, http.StatusInternalServerError, "database error")
			}
			return
		}

		type deptNode struct {
			dept     Department
			children []*deptNode
			employees []Employee
		}

		nodes := map[int]*deptNode{
			rootDept.ID: {dept: rootDept},
		}
		currentIDs := []int{rootDept.ID}

		// строим дерево до depth уровней
		for level := 1; level <= depth; level++ {
			if len(currentIDs) == 0 {
				break
			}

			var children []Department
			if err := db.Where("parent_id IN ?", currentIDs).
				Order("parent_id, name, id").
				Find(&children).Error; err != nil {
				respondError(w, http.StatusInternalServerError, "database error")
				return
			}

			nextIDs := make([]int, 0, len(children))
			for i := range children {
				child := children[i]
				node := &deptNode{dept: child}
				nodes[child.ID] = node

				if child.ParentID != nil {
					if parent, ok := nodes[*child.ParentID]; ok {
						parent.children = append(parent.children, node)
					}
				}

				nextIDs = append(nextIDs, child.ID)
			}

			currentIDs = nextIDs
		}

		// сортируем детей для стабильного ответа
		for _, node := range nodes {
			sort.Slice(node.children, func(i, j int) bool {
				if node.children[i].dept.Name == node.children[j].dept.Name {
					return node.children[i].dept.ID < node.children[j].dept.ID
				}
				return node.children[i].dept.Name < node.children[j].dept.Name
			})
		}

		// загружаем сотрудников
		if includeEmployees {
			deptIDs := make([]int, 0, len(nodes))
			for id := range nodes {
				deptIDs = append(deptIDs, id)
			}

			var employees []Employee
			query := db.Where("department_id IN ?", deptIDs)
			if sortBy == "full_name" {
				query = query.Order("full_name, id")
			} else {
				query = query.Order("created_at, id")
			}

			if err := query.Find(&employees).Error; err != nil {
				respondError(w, http.StatusInternalServerError, "database error")
				return
			}

			empMap := make(map[int][]Employee)
			for _, emp := range employees {
				empMap[emp.DepartmentID] = append(empMap[emp.DepartmentID], emp)
			}

			for _, node := range nodes {
				node.employees = empMap[node.dept.ID]
			}
		}

		var toResponse func(node *deptNode, currentDepth int) DepartmentResponse
		toResponse = func(node *deptNode, currentDepth int) DepartmentResponse {
			resp := DepartmentResponse{
				ID:        node.dept.ID,
				Name:      node.dept.Name,
				ParentID:  node.dept.ParentID,
				CreatedAt: node.dept.CreatedAt,
			}

			if includeEmployees {
				for _, emp := range node.employees {
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
				for _, child := range node.children {
					resp.Children = append(resp.Children, toResponse(child, currentDepth+1))
				}
			}

			return resp
		}

		respondJSON(w, http.StatusOK, toResponse(nodes[rootDept.ID], 0))
	}
}

// 4) Переместить подразделение (обновить)
func updateDepartment(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		deptID, ok := parsePositiveID(idStr)
		if !ok {
			respondError(w, http.StatusBadRequest, "invalid department id")
			return
		}

		var req UpdateDepartmentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if !req.HasName && !req.HasParentID {
			respondError(w, http.StatusBadRequest, "no fields to update")
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

		if req.HasParentID {
			if req.ParentID != nil && *req.ParentID <= 0 {
				respondError(w, http.StatusBadRequest, "parent_id must be a positive integer or null")
				return
			}

			// проверяем существование нового родителя, если он не null
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

			// проверка цикла
			if err := dept.CheckCycle(db, req.ParentID); err != nil {
				respondError(w, http.StatusConflict, err.Error())
				return
			}

			dept.ParentID = req.ParentID
		}

		if req.HasName {
			if req.Name == nil {
				respondError(w, http.StatusBadRequest, "name cannot be null")
				return
			}
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
		deptID, ok := parsePositiveID(idStr)
		if !ok {
			respondError(w, http.StatusBadRequest, "invalid department id")
			return
		}

		mode := r.URL.Query().Get("mode")
		if mode == "" {
			mode = "cascade"
		}
		if mode != "cascade" && mode != "reassign" {
			respondError(w, http.StatusBadRequest, "mode must be 'cascade' or 'reassign'")
			return
		}

		reassignToStr := r.URL.Query().Get("reassign_to_department_id")
		var reassignTo *int
		if reassignToStr != "" {
			val, ok := parsePositiveID(reassignToStr)
			if !ok {
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

		if mode == "reassign" {
			// цель должна существовать
			var targetDept Department
			if err := db.First(&targetDept, *reassignTo).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					respondError(w, http.StatusNotFound, "reassign target department not found")
				} else {
					respondError(w, http.StatusInternalServerError, "database error")
				}
				return
			}

			// нельзя переносить в себя или в своё поддерево
			if err := dept.CheckCycle(db, reassignTo); err != nil {
				respondError(w, http.StatusConflict, err.Error())
				return
			}
		}

		tx := db.Begin()
		if tx.Error != nil {
			respondError(w, http.StatusInternalServerError, "transaction error")
			return
		}

		if mode == "cascade" {
			if err := tx.Delete(&dept).Error; err != nil {
				tx.Rollback()
				respondError(w, http.StatusInternalServerError, err.Error())
				return
			}
		} else {
			// reassign
			if err := tx.Model(&Employee{}).
				Where("department_id = ?", deptID).
				Update("department_id", *reassignTo).Error; err != nil {
				tx.Rollback()
				respondError(w, http.StatusInternalServerError, err.Error())
				return
			}

			if err := tx.Model(&Department{}).
				Where("parent_id = ?", deptID).
				Update("parent_id", *reassignTo).Error; err != nil {
				tx.Rollback()
				respondError(w, http.StatusInternalServerError, err.Error())
				return
			}

			if err := tx.Delete(&dept).Error; err != nil {
				tx.Rollback()
				respondError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}

		if err := tx.Commit().Error; err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
