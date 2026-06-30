package main

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

type Department struct {
	ID        int       `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:200;not null" json:"name"`
	ParentID  *int      `gorm:"index" json:"parent_id"`
	CreatedAt time.Time `json:"created_at"`

	// для построения дерева в памяти
	Children  []Department `gorm:"-" json:"children,omitempty"`
	Employees []Employee   `gorm:"-" json:"employees,omitempty"`
}

type Employee struct {
	ID           int        `gorm:"primaryKey" json:"id"`
	DepartmentID  int        `gorm:"not null;index" json:"department_id"`
	FullName     string     `gorm:"size:200;not null" json:"full_name"`
	Position     string     `gorm:"size:200;not null" json:"position"`
	HiredAt      *time.Time `json:"hired_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

// BeforeSave для подразделений: trim + валидация + уникальность в рамках parent_id
func (d *Department) BeforeSave(tx *gorm.DB) error {
	d.Name = strings.TrimSpace(d.Name)
	if d.Name == "" {
		return errors.New("name cannot be empty")
	}
	if len(d.Name) > 200 {
		return errors.New("name too long (max 200)")
	}

	var count int64
	query := tx.Model(&Department{}).Where("name = ?", d.Name)

	if d.ParentID == nil {
		query = query.Where("parent_id IS NULL")
	} else {
		query = query.Where("parent_id = ?", *d.ParentID)
	}

	if d.ID != 0 {
		query = query.Where("id <> ?", d.ID)
	}

	if err := query.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return errors.New("department name must be unique within the same parent")
	}

	return nil
}

// BeforeSave для сотрудников: trim + валидация
func (e *Employee) BeforeSave(tx *gorm.DB) error {
	e.FullName = strings.TrimSpace(e.FullName)
	if e.FullName == "" {
		return errors.New("full_name cannot be empty")
	}
	if len(e.FullName) > 200 {
		return errors.New("full_name too long (max 200)")
	}

	e.Position = strings.TrimSpace(e.Position)
	if e.Position == "" {
		return errors.New("position cannot be empty")
	}
	if len(e.Position) > 200 {
		return errors.New("position too long (max 200)")
	}

	return nil
}

// CheckCycle проверяет, не приведёт ли newParentID к циклу
func (d *Department) CheckCycle(db *gorm.DB, newParentID *int) error {
	if newParentID == nil {
		return nil
	}
	if *newParentID == d.ID {
		return errors.New("cannot be parent of itself")
	}

	queue := []int{d.ID}
	visited := map[int]bool{}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if visited[current] {
			continue
		}
		visited[current] = true

		var children []int
		if err := db.Model(&Department{}).
			Where("parent_id = ?", current).
			Pluck("id", &children).Error; err != nil {
			return err
		}

		for _, childID := range children {
			if childID == *newParentID {
				return errors.New("cannot move department inside its own subtree")
			}
			queue = append(queue, childID)
		}
	}

	return nil
}
