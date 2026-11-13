package routes

import (
	"context"
)

type healthzBody struct {
	Status string `json:"status" example:"ok"`
}

// HealthzOutput - ответ /healthz.
type HealthzOutput struct {
	Body healthzBody
}

// HealthzHumaHandler - /healthz через Huma: попадает в спеку, валидация тривиальна.
func HealthzHumaHandler(_ context.Context, _ *struct{}) (*HealthzOutput, error) {
	out := &HealthzOutput{}
	out.Body.Status = "ok"
	return out, nil
}
