package channel

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testMaxChannels = 500

func mustCode(t *testing.T, s string) Code {
	t.Helper()

	c, err := ParseCode(s)
	require.NoError(t, err)
	return c
}

// codeSeq - генератор кодов, который отдаёт заранее заданную
// последовательность и считает вызовы.
type codeSeq struct {
	mu    sync.Mutex
	codes []Code
	calls int
}

func newCodeSeq(t *testing.T, codes ...string) *codeSeq {
	t.Helper()

	s := &codeSeq{}
	for _, c := range codes {
		s.codes = append(s.codes, mustCode(t, c))
	}
	return s
}

func (s *codeSeq) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *codeSeq) next() (Code, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.calls >= len(s.codes) {
		return Code{}, errors.New("codeSeq: последовательность кончилась")
	}
	c := s.codes[s.calls]
	s.calls++
	return c, nil
}

func repeat(code string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = code
	}
	return out
}

// BR-1.3: реестр отдаёт канал с хозяином на west и токеном хозяина.
// Рассадку подробно проверяют тесты канала (TestNewChannel_*).
func TestRegistry_Create(t *testing.T) {
	t.Parallel()

	for _, mode := range []GameMode{GameModeCoop, GameModeSolo} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()

			r := NewRegistry(testMaxChannels)

			ch, tok, err := r.Create(mode)
			require.NoError(t, err)
			require.NotNil(t, ch)
			require.False(t, tok.IsEmpty())

			assert.Equal(t, mode, ch.Mode())
			assert.False(t, ch.Code().IsEmpty())
			assert.NotEmpty(t, ch.ID())

			post, ok := ch.PostOf(tok)
			require.True(t, ok)
			assert.Equal(t, PostWest, post)

			assert.Equal(t, 1, r.Len())
		})
	}
}

// BR-1.4: вход по коду - реестр находит канал, место выдаёт канал.
func TestRegistry_ByCode_ThenJoin(t *testing.T) {
	t.Parallel()

	r := NewRegistry(testMaxChannels)

	ch, _, err := r.Create(GameModeCoop)
	require.NoError(t, err)

	found, err := r.ByCode(ch.Code())
	require.NoError(t, err)
	require.Same(t, ch, found)

	tok, err := found.Join()
	require.NoError(t, err)

	post, ok := ch.PostOf(tok)
	require.True(t, ok)
	assert.Equal(t, PostEast, post)
}

// BR-1.5, BR-2.2: нет канала с таким кодом (в том числе с ведущим нулём -
// формат он проходит, но такие коды не выдаются).
func TestRegistry_ByCode_Unknown(t *testing.T) {
	t.Parallel()

	r := NewRegistry(testMaxChannels, WithCodeGenerator(newCodeSeq(t, "111111").next))

	_, _, err := r.Create(GameModeCoop)
	require.NoError(t, err)

	for _, code := range []string{"222222", "011111"} {
		ch, err := r.ByCode(mustCode(t, code))
		require.ErrorIs(t, err, ErrNotFound, code)
		assert.Nil(t, ch, code)
	}
}

// BR-1.5: одиночный канал по коду не находится, по id - находится
// (сокет хозяина подключается по id).
func TestRegistry_Lookup_Solo(t *testing.T) {
	t.Parallel()

	r := NewRegistry(testMaxChannels)

	ch, _, err := r.Create(GameModeSolo)
	require.NoError(t, err)

	byCode, err := r.ByCode(ch.Code())
	require.ErrorIs(t, err, ErrNotFound)
	assert.Nil(t, byCode)

	byID, err := r.ByID(ch.ID())
	require.NoError(t, err)
	assert.Same(t, ch, byID)
}

// EFR-1.c: поиск по коду и по id, удаление.
func TestRegistry_Lookup(t *testing.T) {
	t.Parallel()

	r := NewRegistry(testMaxChannels)

	ch, _, err := r.Create(GameModeCoop)
	require.NoError(t, err)

	byCode, err := r.ByCode(ch.Code())
	require.NoError(t, err)
	assert.Same(t, ch, byCode)

	byID, err := r.ByID(ch.ID())
	require.NoError(t, err)
	assert.Same(t, ch, byID)

	_, err = r.ByID("c_unknown")
	require.ErrorIs(t, err, ErrNotFound)

	r.Remove(ch.ID())
	assert.Equal(t, 0, r.Len())

	_, err = r.ByCode(ch.Code())
	require.ErrorIs(t, err, ErrNotFound)
	_, err = r.ByID(ch.ID())
	require.ErrorIs(t, err, ErrNotFound)

	assert.NotPanics(t, func() { r.Remove(ch.ID()) }, "повторное удаление - не ошибка")
	assert.NotPanics(t, func() { r.Remove("c_unknown") }, "удаление неизвестного - не ошибка")
}

// BR-1.7: больше GAME_MAX_CHANNELS каналов не открыть.
func TestRegistry_TooManyChannels(t *testing.T) {
	t.Parallel()

	r := NewRegistry(2)

	first, _, err := r.Create(GameModeCoop)
	require.NoError(t, err)
	_, _, err = r.Create(GameModeSolo)
	require.NoError(t, err)

	ch, tok, err := r.Create(GameModeCoop)
	require.ErrorIs(t, err, ErrTooManyChannels)
	assert.Nil(t, ch)
	assert.True(t, tok.IsEmpty())
	assert.Equal(t, 2, r.Len())

	r.Remove(first.ID())
	assert.Equal(t, 1, r.Len())

	_, _, err = r.Create(GameModeCoop)
	require.NoError(t, err, "после удаления место освободилось")
}

// BR-1.7: лимит не тратит попытки генерации кода.
func TestRegistry_TooManyChannels_NoCodeGenerated(t *testing.T) {
	t.Parallel()

	gen := newCodeSeq(t, "111111", "222222")
	r := NewRegistry(1, WithCodeGenerator(gen.next))

	_, _, err := r.Create(GameModeCoop)
	require.NoError(t, err)

	_, _, err = r.Create(GameModeCoop)
	require.ErrorIs(t, err, ErrTooManyChannels)
	assert.Equal(t, 1, gen.Calls())
}

// BR-1.1: при коллизии кода - новая попытка, всего не больше 10.
// Генератор возвращает занятый код 9 раз - успех с 10-й попытки.
func TestRegistry_Create_CodeCollision_9(t *testing.T) {
	t.Parallel()

	codes := append([]string{"111111"}, repeat("111111", 9)...)
	codes = append(codes, "222222")
	gen := newCodeSeq(t, codes...)
	r := NewRegistry(testMaxChannels, WithCodeGenerator(gen.next))

	_, _, err := r.Create(GameModeCoop)
	require.NoError(t, err)

	ch, _, err := r.Create(GameModeCoop)
	require.NoError(t, err)
	assert.Equal(t, "222222", ch.Code().String())
	assert.Equal(t, 1+10, gen.Calls())
	assert.Equal(t, 2, r.Len())
}

// BR-1.1: генератор возвращает занятый код 10 раз - ошибка, 11-й попытки нет.
func TestRegistry_Create_CodeCollision_10(t *testing.T) {
	t.Parallel()

	codes := append([]string{"111111"}, repeat("111111", 10)...)
	codes = append(codes, "222222")
	gen := newCodeSeq(t, codes...)
	r := NewRegistry(testMaxChannels, WithCodeGenerator(gen.next))

	_, _, err := r.Create(GameModeCoop)
	require.NoError(t, err)

	ch, tok, err := r.Create(GameModeCoop)
	require.ErrorIs(t, err, ErrCodeCollision)
	require.NotErrorIs(t, err, ErrTooManyChannels)
	assert.Nil(t, ch)
	assert.True(t, tok.IsEmpty())
	assert.Equal(t, 1+10, gen.Calls())
	assert.Equal(t, 1, r.Len(), "неудачный канал не попал в реестр")
}

// BR-1.1: ошибка генератора не превращается в «успех» с пустым кодом.
func TestRegistry_Create_GeneratorError(t *testing.T) {
	t.Parallel()

	r := NewRegistry(testMaxChannels, WithCodeGenerator(newCodeSeq(t).next))

	ch, tok, err := r.Create(GameModeCoop)
	require.Error(t, err)
	assert.Nil(t, ch)
	assert.True(t, tok.IsEmpty())
	assert.Equal(t, 0, r.Len())
}

// BR-1.1: код уникален среди открытых каналов, после удаления снова свободен.
func TestRegistry_Create_CodeReusedAfterRemove(t *testing.T) {
	t.Parallel()

	r := NewRegistry(testMaxChannels, WithCodeGenerator(newCodeSeq(t, "111111", "111111").next))

	first, _, err := r.Create(GameModeCoop)
	require.NoError(t, err)
	r.Remove(first.ID())

	second, _, err := r.Create(GameModeCoop)
	require.NoError(t, err)
	assert.Equal(t, "111111", second.Code().String())
	assert.NotEqual(t, first.ID(), second.ID())
}

// BR-1.1: коды solo и coop живут в одном пространстве - solo тоже занимает код.
func TestRegistry_Create_SoloOccupiesCode(t *testing.T) {
	t.Parallel()

	gen := newCodeSeq(t, "111111", "111111", "222222")
	r := NewRegistry(testMaxChannels, WithCodeGenerator(gen.next))

	_, _, err := r.Create(GameModeSolo)
	require.NoError(t, err)

	ch, _, err := r.Create(GameModeCoop)
	require.NoError(t, err)
	assert.Equal(t, "222222", ch.Code().String())
}

// BR-2.4 (закладывается здесь): id - отдельный случайный идентификатор,
// с кодом не совпадает и не содержит его.
func TestRegistry_ChannelID(t *testing.T) {
	t.Parallel()

	r := NewRegistry(testMaxChannels)

	ids := make(map[string]struct{})
	for range 100 {
		ch, _, err := r.Create(GameModeCoop)
		require.NoError(t, err)

		id := ch.ID()
		assert.True(t, strings.HasPrefix(id, "c_"), id)
		assert.NotContains(t, id, ch.Code().String())

		_, dup := ids[id]
		require.False(t, dup, "повтор id")
		ids[id] = struct{}{}
	}
}

// BR-1.8: 100 горутин создают каналы одновременно, коды и id уникальны.
func TestRegistry_Create_Concurrent(t *testing.T) {
	t.Parallel()

	const n = 100
	r := NewRegistry(testMaxChannels)

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		codes = make(map[string]struct{}, n)
		ids   = make(map[string]struct{}, n)
	)
	for i := range n {
		mode := GameModeCoop
		if i%2 == 0 {
			mode = GameModeSolo
		}
		wg.Go(func() {
			ch, _, err := r.Create(mode)
			if err != nil {
				t.Errorf("create: %v", err) // require.NoError нельзя вне главной горутины
				return
			}
			_, _ = r.ByID(ch.ID())
			_, _ = r.ByCode(ch.Code())
			_ = r.Len()

			mu.Lock()
			defer mu.Unlock()
			codes[ch.Code().String()] = struct{}{}
			ids[ch.ID()] = struct{}{}
		})
	}
	wg.Wait()

	assert.Len(t, codes, n, "коды уникальны")
	assert.Len(t, ids, n, "id уникальны")
	assert.Equal(t, n, r.Len())
}

// BR-1.7, BR-1.8: при гонке за последние места лимит не превышается.
func TestRegistry_Create_ConcurrentLimit(t *testing.T) {
	t.Parallel()

	const limit, n = 10, 100
	r := NewRegistry(limit)

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		ok   int
		over int
	)
	for range n {
		wg.Go(func() {
			_, _, err := r.Create(GameModeCoop)

			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrTooManyChannels):
				over++
			default:
				t.Errorf("неожиданная ошибка: %v", err)
			}
		})
	}
	wg.Wait()

	assert.Equal(t, limit, ok)
	assert.Equal(t, n-limit, over)
	assert.Equal(t, limit, r.Len())
}

// BR-1.8: создание, поиск и удаление вперемешку - -race молчит,
// счётчик сходится.
func TestRegistry_CreateRemove_Concurrent(t *testing.T) {
	t.Parallel()

	const n = 100
	r := NewRegistry(testMaxChannels)

	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			ch, _, err := r.Create(GameModeCoop)
			if err != nil {
				t.Errorf("create: %v", err) // require.NoError нельзя вне главной горутины
				return
			}
			_, _ = r.ByCode(ch.Code())
			r.Remove(ch.ID())
		})
	}
	wg.Wait()

	assert.Equal(t, 0, r.Len())
}
