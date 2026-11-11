package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"sync"

	"github.com/danielgtaylor/huma/v2"

	"github.com/uair10/efir_backend/internal/infrastructure/logging"
)

var installErrorPolicyOnce sync.Once //nolint:gochecknoglobals // huma.NewErrorWithContext глобальна, ставим один раз

// installErrorPolicy один раз оборачивает фабрику ошибок huma.
// Модель ответа остаётся стандартной (huma.ErrorModel), меняется только содержимое:
//   - 5xx пишется в лог запроса с текстом исходной ошибки, а клиенту уходит без подробностей;
//   - из errors[] убираются значения из запроса: huma по умолчанию кладёт их в errors[].value,
//     а там может оказаться токен или другой секрет.
func installErrorPolicy() {
	installErrorPolicyOnce.Do(func() {
		base := huma.NewErrorWithContext
		huma.NewErrorWithContext = func(ctx huma.Context, status int, msg string, errs ...error) huma.StatusError {
			if status >= http.StatusInternalServerError {
				attrs := []any{slog.Int("status", status), slog.String("detail", msg)}
				if err := errors.Join(errs...); err != nil {
					attrs = append(attrs, slog.String("error", err.Error()))
				}
				logging.FromCtx(ctx.Context()).ErrorContext(ctx.Context(), "internal error", attrs...)
				return base(ctx, status, http.StatusText(status))
			}
			return base(ctx, status, msg, withoutValues(errs)...)
		}
	})
}

// withoutValues копирует подробности ошибок без значений из запроса.
func withoutValues(errs []error) []error {
	out := make([]error, len(errs))
	for i, err := range errs {
		var d huma.ErrorDetailer
		if errors.As(err, &d) {
			detail := d.ErrorDetail()
			out[i] = &huma.ErrorDetail{Message: detail.Message, Location: detail.Location}
			continue
		}
		out[i] = err
	}
	return out
}
