// Package game содержит правила и модель смены.
package game

// Vec2 - точка в клетках карты (может быть дробной).
type Vec2 struct{ X, Y float64 }

// Cell - клетка карты (целые координаты).
type Cell struct{ X, Y int }
