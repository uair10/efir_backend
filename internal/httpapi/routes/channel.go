package routes

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/uair10/efir_backend/internal/channel"
)

// channelBody - ответ на создание канала и вход по коду (раздел 2 контракта).
type channelBody struct {
	ChannelID string `json:"channelId" doc:"ID канала"` //nolint:tagliatelle // имя поля из контракта с фронтендом
	Code      string `json:"code"      doc:"6-значный код входа"`
	Token     string `json:"token"     doc:"Токен игрока"`
}

// createChannelBody - тело запроса на создание канала.
type createChannelBody struct {
	Mode channel.GameMode `json:"mode" enum:"solo,coop" doc:"Режим: solo или coop"`
}

// CreateChannelInput - запрос POST /api/channels.
type CreateChannelInput struct {
	Body createChannelBody
}

// CreateChannelOutput - ответ 201 на создание канала.
type CreateChannelOutput struct {
	Status int
	Body   channelBody
}

// JoinChannelInput - запрос POST /api/channels/{code}/players.
type JoinChannelInput struct {
	Code channel.Code `path:"code" pattern:"^[0-9]{6}$" doc:"6-значный код канала"`
}

// JoinChannelOutput - ответ 200 на вход по коду.
type JoinChannelOutput struct {
	Body channelBody
}

// Channels - обработчики каналов. Реестр один на процесс: его создаёт main
// и передаёт сюда, все запросы работают с одним и тем же экземпляром.
type Channels struct {
	registry *channel.Registry
}

// NewChannels собирает обработчики над общим реестром каналов.
func NewChannels(registry *channel.Registry) *Channels {
	return &Channels{registry: registry}
}

// Create открывает канал и отдает хосту код и токен.
func (c *Channels) Create(_ context.Context, in *CreateChannelInput) (*CreateChannelOutput, error) {
	ch, tok, err := c.registry.Create(in.Body.Mode)
	if err != nil {
		return nil, toHTTPError(err)
	}
	return &CreateChannelOutput{
		Status: http.StatusCreated,
		Body:   newChannelBody(ch, tok),
	}, nil
}

// Join сажает гостя на свободный пост канала с кодом in.Code.
func (c *Channels) Join(_ context.Context, in *JoinChannelInput) (*JoinChannelOutput, error) {
	ch, err := c.registry.ByCode(in.Code)
	if err != nil {
		return nil, toHTTPError(err)
	}
	tok, err := ch.Join()
	if err != nil {
		return nil, toHTTPError(err)
	}
	return &JoinChannelOutput{Body: newChannelBody(ch, tok)}, nil
}

func newChannelBody(ch *channel.Channel, tok channel.Token) channelBody {
	return channelBody{
		ChannelID: ch.ID(),
		Code:      ch.Code().String(),
		Token:     tok.Reveal(),
	}
}

// toHTTPError отображает доменные ошибки на статусы.
// Остальное уходит как 500: подробности попадут в лог, не клиенту.
func toHTTPError(err error) error {
	switch {
	case errors.Is(err, channel.ErrNotFound):
		return huma.Error404NotFound("channel not found")
	case errors.Is(err, channel.ErrFull):
		return huma.Error409Conflict("channel is full")
	case errors.Is(err, channel.ErrTooManyChannels):
		return huma.Error503ServiceUnavailable("too many channels")
	default:
		return fmt.Errorf("channels: %w", err)
	}
}
