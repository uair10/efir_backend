package channel

// Тесты логов канала (EFR-24): по channel_id собирается история канала,
// события создания и входа связаны с запросом, токены не попадают в лог.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uair10/efir_backend/internal/game"

	"github.com/uair10/efir_backend/internal/infrastructure/logging"
)

// syncBuffer - bytes.Buffer, в который безопасно пишут несколько горутин.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// records разбирает JSON-записи лога.
func (b *syncBuffer) records(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	sc := bufio.NewScanner(bytes.NewBufferString(b.String()))
	for sc.Scan() {
		var rec map[string]any
		require.NoError(t, json.Unmarshal(sc.Bytes(), &rec))
		out = append(out, rec)
	}
	return out
}

// BR-24.2, BR-24.5, NFR-3: создать - войти - подключить двоих - закрыть.
// У всех записей один channel_id, у создания и входа - request_id своего
// запроса, токенов в логе нет даже на debug.
func TestLog_ChannelHistory(t *testing.T) {
	t.Parallel()

	var buf syncBuffer
	base := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	// reqCtx - контекст запроса, как его собирает middleware.RequestID.
	reqCtx := func(id string) context.Context {
		return logging.WithLogger(t.Context(), base.With(logging.RequestID(id)))
	}

	r := NewRegistry(testMaxChannels, WithLogger(base))
	ch, host, err := r.Create(reqCtx("req-create"), game.ModeCoop)
	require.NoError(t, err)
	guest, err := ch.Join(reqCtx("req-join"))
	require.NoError(t, err)

	hostSock, guestSock := newFakeSocket(), newFakeSocket()
	hostConn, err := ch.Connect(reqCtx("req-ws-host"), host, hostSock)
	require.NoError(t, err)
	_, err = ch.Connect(reqCtx("req-ws-guest"), guest, guestSock)
	require.NoError(t, err)
	hostConn.Receive([]byte(`{"type":"set_callsign","callsign":"Второй"}`)) // отказ - debug
	for {
		m := <-hostSock.msgs // лобби до отказа пропускаем; канал отвечает сразу
		if _, ok := m.Payload.(ErrorMessage); ok {
			break
		}
	}
	r.Close()

	byMsg := map[string]map[string]any{}
	for _, rec := range buf.records(t) {
		assert.Equal(t, ch.ID(), rec[logging.KeyChannelID], "запись без channel_id: %v", rec)
		msg, ok := rec["msg"].(string)
		require.True(t, ok)
		byMsg[msg] = rec
	}

	require.Contains(t, byMsg, "channel created")
	assert.Equal(t, "req-create", byMsg["channel created"][logging.KeyRequestID])
	assert.Equal(t, "coop", byMsg["channel created"]["mode"])

	require.Contains(t, byMsg, "guest joined")
	assert.Equal(t, "req-join", byMsg["guest joined"][logging.KeyRequestID])
	assert.Equal(t, "east", byMsg["guest joined"][logging.KeyPost])

	require.Contains(t, byMsg, "command rejected")
	assert.Equal(t, "req-ws-host", byMsg["command rejected"][logging.KeyRequestID])
	assert.Equal(t, "west", byMsg["command rejected"][logging.KeyPost])
	assert.Equal(t, "set_callsign", byMsg["command rejected"]["kind"])

	require.Contains(t, byMsg, "channel closed")
	assert.Equal(t, "shutdown", byMsg["channel closed"]["reason"])

	logs := buf.String()
	for _, tok := range []Token{host, guest} {
		assert.NotContains(t, logs, tok.Reveal(), "токен в логе")
	}
}
