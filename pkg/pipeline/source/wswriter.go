package source

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/gorilla/websocket"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/h264writer"
	"github.com/pion/webrtc/v3/pkg/media/ivfwriter"
	"github.com/pion/webrtc/v3/pkg/media/oggwriter"

	"github.com/livekit/livekit-egress/pkg/pipeline/params"
	"github.com/livekit/protocol/logger"
	lksdk "github.com/livekit/server-sdk-go"
	"github.com/livekit/server-sdk-go/pkg/samplebuilder"

	"github.com/livekit/livekit-egress/pkg/errors"
)

type wsWriter struct {
	sb      *samplebuilder.SampleBuilder
	track   *webrtc.TrackRemote
	cs      *clockSync
	maxLate time.Duration

	started bool
	logger  logger.Logger
	writer  media.Writer
	done    chan struct{}
	closed  bool
}

func newWsWriter(p *params.Params, track *webrtc.TrackRemote, rp *lksdk.RemoteParticipant, l logger.Logger, cs *clockSync) (*wsWriter, error) {
	w := &wsWriter{
		track:  track,
		cs:     cs,
		logger: logger.Logger(logr.Logger(l).WithValues("trackID", track.ID())),
		done:   make(chan struct{}),
		closed: false,
	}
	var err error

	// Create WS sink
	if !strings.ContainsAny(p.OutWebsocketURL, "ws") {
		return nil, errors.New("invalid WS url")
	}
	sink, err := newWebsocketSink(p.OutWebsocketURL, track.Codec().MimeType)
	if err != nil {
		return nil, err
	}

	switch {
	case strings.EqualFold(track.Codec().MimeType, MimeTypeVP8):
		w.sb = samplebuilder.New(
			maxVideoLate, &codecs.VP8Packet{}, track.Codec().ClockRate,
			samplebuilder.WithPacketDroppedHandler(func() { rp.WritePLI(track.SSRC()) }),
		)
		w.writer, err = ivfwriter.NewWith(sink)

	case strings.EqualFold(track.Codec().MimeType, MimeTypeH264):
		w.sb = samplebuilder.New(
			maxVideoLate, &codecs.H264Packet{}, track.Codec().ClockRate,
			samplebuilder.WithPacketDroppedHandler(func() { rp.WritePLI(track.SSRC()) }),
		)
		w.writer = h264writer.NewWith(sink)

	case strings.EqualFold(track.Codec().MimeType, MimeTypeOpus):
		w.sb = samplebuilder.New(maxAudioLate, &codecs.OpusPacket{}, track.Codec().ClockRate)
		w.writer, err = oggwriter.NewWith(sink, 48000, track.Codec().Channels)

	default:
		err = errors.ErrNotSupported(track.Codec().MimeType)
	}
	if err != nil {
		return nil, err
	}

	// TODO: start a goroutine which subscribes to track notifications (muted / unmuted) and write to WS

	go w.start()
	return w, nil
}

func (w *wsWriter) start() {
	defer func() {
		if err := w.writer.Close(); err != nil {
			w.logger.Errorw("could not close file writer", err)
		}
	}()

	for {
		select {
		case <-w.done:
			// drain sample builder
			_ = w.writePackets(true)
			return

		default:
			pkt, _, err := w.track.ReadRTP()
			if err != nil {
				if errors.Is(err, io.EOF) {
					w.stop()
					continue
				} else {
					w.logger.Errorw("could not read from track", err)
					return
				}
			}

			if !w.started {
				w.cs.GetOrSetStartTime(time.Now().UnixNano())
				w.started = true
			}

			w.sb.Push(pkt)
			if err = w.writePackets(false); err != nil {
				return
			}
		}
	}
}

func (w *wsWriter) writePackets(force bool) error {
	// If the connection is already closed, just return
	if w.closed {
		return nil
	}
	var pkts []*rtp.Packet
	if force {
		pkts = w.sb.ForcePopPackets()
	} else {
		pkts = w.sb.PopPackets()
	}

	for _, pkt := range pkts {
		if err := w.writer.WriteRTP(pkt); err != nil {
			w.logger.Errorw("could not write to file", err)
			return err
		}
	}

	return nil
}

func (w *wsWriter) stop() {
	select {
	case <-w.done:
		return
	default:
		close(w.done)
	}
}

type websocketSink struct {
	conn *websocket.Conn
}

func newWebsocketSink(url string, mimeType string) (io.Writer, error) {
	// Set Content-Type in header
	header := http.Header{}
	header.Set("Content-Type", mimeType)

	conn, _, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		return nil, err
	}

	return &websocketSink{conn}, nil
}

func (s *websocketSink) Write(p []byte) (int, error) {
	return len(p), s.conn.WriteMessage(websocket.BinaryMessage, p)
}

func (s *websocketSink) Close() error {
	// Write close message for graceful disconnection
	s.conn.WriteMessage(websocket.CloseMessage, nil)

	// Terminate connection
	return s.conn.Close()
}
