package channel

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestChannel - канал вне реестра. Места раздаёт сам канал,
// реестр его только находит.
func newTestChannel(t *testing.T, mode GameMode) (*Channel, Token) {
	t.Helper()

	ch, tok, err := newChannel("c_test", mustCode(t, "482117"), mode)
	require.NoError(t, err)
	require.NotNil(t, ch)
	require.False(t, tok.IsEmpty())
	return ch, tok
}

func TestNewChannel_Fields(t *testing.T) {
	t.Parallel()

	ch, _ := newTestChannel(t, GameModeCoop)

	assert.Equal(t, "c_test", ch.ID())
	assert.Equal(t, "482117", ch.Code().String())
	assert.Equal(t, GameModeCoop, ch.Mode())
}

// BR-1.3: открывший канал занимает west с позывным «Первый».
func TestNewChannel_Coop(t *testing.T) {
	t.Parallel()

	ch, tok := newTestChannel(t, GameModeCoop)

	host, ok := ch.Player(PostWest)
	require.True(t, ok)
	assert.Equal(t, CallsignFirst, host.Callsign)
	assert.False(t, host.Bot)
	assert.False(t, host.Ready)

	_, ok = ch.Player(PostEast)
	assert.False(t, ok, "в coop east свободен до входа гостя")

	post, ok := ch.PostOf(tok)
	require.True(t, ok, "токен хозяина пускает на его пост")
	assert.Equal(t, PostWest, post)
}

// BR-1.3: в одиночной смене east сразу занимает бот «Второй», готовый.
func TestNewChannel_Solo(t *testing.T) {
	t.Parallel()

	ch, tok := newTestChannel(t, GameModeSolo)

	host, ok := ch.Player(PostWest)
	require.True(t, ok)
	assert.Equal(t, CallsignFirst, host.Callsign)
	assert.False(t, host.Bot)

	bot, ok := ch.Player(PostEast)
	require.True(t, ok)
	assert.Equal(t, CallsignSecond, bot.Callsign)
	assert.True(t, bot.Bot)
	assert.True(t, bot.Ready)

	post, ok := ch.PostOf(tok)
	require.True(t, ok)
	assert.Equal(t, PostWest, post)
}

// BR-1.4: гость занимает east и получает первый свободный позывной.
// Случай «у хозяина Второй -> гость Первый» покрыт в TestFirstFreeCallsign:
// сменить позывной хозяина можно только командой лобби (EFR-4).
func TestChannel_Join(t *testing.T) {
	t.Parallel()

	ch, hostTok := newTestChannel(t, GameModeCoop)

	guestTok, err := ch.Join()
	require.NoError(t, err)
	require.False(t, guestTok.IsEmpty())
	assert.False(t, guestTok.Equal(hostTok), "у каждого игрока свой токен")

	guest, ok := ch.Player(PostEast)
	require.True(t, ok)
	assert.Equal(t, CallsignSecond, guest.Callsign)
	assert.False(t, guest.Bot)
	assert.False(t, guest.Ready)

	post, ok := ch.PostOf(guestTok)
	require.True(t, ok)
	assert.Equal(t, PostEast, post)

	post, ok = ch.PostOf(hostTok)
	require.True(t, ok)
	assert.Equal(t, PostWest, post)
}

// BR-1.6: оба поста заняты.
func TestChannel_Join_Full(t *testing.T) {
	t.Parallel()

	ch, _ := newTestChannel(t, GameModeCoop)

	guestTok, err := ch.Join()
	require.NoError(t, err)

	tok, err := ch.Join()
	require.ErrorIs(t, err, ErrFull)
	assert.True(t, tok.IsEmpty())

	post, ok := ch.PostOf(guestTok)
	require.True(t, ok, "гость не вытеснен")
	assert.Equal(t, PostEast, post)
}

// BR-1.5, BR-1.6: в одиночный канал не войти и напрямую - east занят ботом.
// Снаружи (по коду) его скрывает реестр, см. TestRegistry_Lookup_Solo.
func TestChannel_Join_Solo(t *testing.T) {
	t.Parallel()

	ch, _ := newTestChannel(t, GameModeSolo)

	tok, err := ch.Join()
	require.ErrorIs(t, err, ErrFull)
	assert.True(t, tok.IsEmpty())

	bot, ok := ch.Player(PostEast)
	require.True(t, ok)
	assert.True(t, bot.Bot, "бот на месте")
}

// BR-1.6, BR-1.8: из многих одновременных входов проходит ровно один.
// Параллельно читаем посты, чтобы -race увидел гонку чтения с записью.
func TestChannel_Join_Concurrent(t *testing.T) {
	t.Parallel()

	ch, hostTok := newTestChannel(t, GameModeCoop)

	const n = 50
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		ok   int
		full int
		toks []Token
	)
	for range n {
		wg.Go(func() {
			tok, err := ch.Join()
			_, _ = ch.Player(PostEast)
			_, _ = ch.PostOf(hostTok)

			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
				toks = append(toks, tok)
			case errors.Is(err, ErrFull):
				full++
			default:
				t.Errorf("неожиданная ошибка: %v", err)
			}
		})
	}
	wg.Wait()

	assert.Equal(t, 1, ok)
	assert.Equal(t, n-1, full)
	require.Len(t, toks, 1)

	post, found := ch.PostOf(toks[0])
	require.True(t, found, "токен победителя привязан к east")
	assert.Equal(t, PostEast, post)
}

// BR-1.2: чужой и пустой токен никуда не пускают.
func TestChannel_PostOf_Foreign(t *testing.T) {
	t.Parallel()

	ch, _ := newTestChannel(t, GameModeCoop)
	_, otherTok := newTestChannel(t, GameModeCoop)

	_, ok := ch.PostOf(otherTok)
	assert.False(t, ok, "токен другого канала")

	_, ok = ch.PostOf(Token{})
	assert.False(t, ok, "пустой токен при свободном east")

	fresh, err := newToken()
	require.NoError(t, err)
	_, ok = ch.PostOf(fresh)
	assert.False(t, ok, "случайный токен")
}

// BR-1.2: у бота нет токена, но пустой токен не должен попадать на его пост.
func TestChannel_PostOf_EmptyTokenSolo(t *testing.T) {
	t.Parallel()

	ch, _ := newTestChannel(t, GameModeSolo)

	_, ok := ch.PostOf(Token{})
	assert.False(t, ok, "пустой токен не равен «токену» бота")
}
