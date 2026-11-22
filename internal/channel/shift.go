package channel

import "github.com/uair10/efir_backend/internal/game"

// sendShiftSnapshot sends the briefing view on connection or reconnect expiry.
func (r *room) sendShiftSnapshot(conn *Connection) {
	snapshot := game.BriefingSnapshot(conn.post, r.mode, r.shiftPaused)
	conn.sock.Send(Message{Payload: snapshot})
}
