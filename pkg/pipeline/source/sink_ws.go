package source

import (
	"context"
	"github.com/gorilla/websocket"
	"github.com/livekit/protocol/logger"
	"io"
	"net/http"
)

type LiveKitEvent string

const (
	ParticipantMuted   LiveKitEvent = "ParticipantMuted"
	ParticipantUnmuted LiveKitEvent = "ParticipantUnmuted"
)

type websocketSink struct {
	closed bool

	ctx    context.Context
	cancel context.CancelFunc

	conn   *websocket.Conn
	logger logger.Logger
}

func createWebsocketSink(
	url string,
	mimeType string,
	logger logger.Logger,
	notifyChan chan LiveKitEvent,
) (io.WriteCloser, error) {

	// Set Content-Type in header
	header := http.Header{}
	header.Set("Content-Type", mimeType)

	conn, _, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		return nil, err
	}

	s := &websocketSink{
		conn:   conn,
		logger: logger,
	}
	s.ctx, s.cancel = context.WithCancel(context.TODO())

	// Start a goroutine to listen to events
	go s.listen(notifyChan)

	return s, nil
}

func (s websocketSink) Write(p []byte) (n int, err error) {
	return len(p), s.conn.WriteMessage(websocket.BinaryMessage, p)
}

// Close Must be idempotent
func (s websocketSink) Close() error {
	if s.closed {
		return nil
	}
	defer func() {
		s.closed = true
	}()
	s.cancel()
	return s.conn.Close()
}

func (s *websocketSink) listen(channel chan LiveKitEvent) {
	var err error
	for {
		select {
		case ev := <-channel:
			switch ev {
			case ParticipantMuted:
				err = s.conn.WriteMessage(websocket.TextMessage, []byte("muted: true"))
			case ParticipantUnmuted:
				err = s.conn.WriteMessage(websocket.TextMessage, []byte("muted: false"))
			}
			if err != nil {
				s.logger.Errorw("cannot write text message on WS", err)
				err = nil // clear error after logging
			}
		case <-s.ctx.Done():
			err = s.conn.WriteMessage(websocket.CloseMessage, nil)
			return
		}
	}
}
