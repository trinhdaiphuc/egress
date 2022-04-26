//go:build integration
// +build integration

package test

import (
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/lithammer/shortuuid/v3"
	"github.com/stretchr/testify/require"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/utils"
	lksdk "github.com/livekit/server-sdk-go"
	"log"

	"github.com/livekit/livekit-egress/pkg/config"
)

func testTrackComposite(t *testing.T, conf *config.Config, room *lksdk.Room) {
	testTrackCompositeFile(t, conf, room, "opus", "vp8", []*testCase{
		{
			name:       "tc-vp8-mp4",
			fileType:   livekit.EncodedFileType_MP4,
			filePrefix: "tc-vp8",
		},
		{
			name:       "tc-opus-only-ogg",
			audioOnly:  true,
			fileType:   livekit.EncodedFileType_OGG,
			filePrefix: "tc-opus-only",
		},
	})

	testTrackCompositeFile(t, conf, room, "opus", "h264", []*testCase{
		{
			name:       "tc-h264-mp4",
			fileType:   livekit.EncodedFileType_MP4,
			filePrefix: "tc-h264",
		},
		{
			name:       "tc-h264-only-mp4",
			videoOnly:  true,
			fileType:   livekit.EncodedFileType_MP4,
			filePrefix: "tc-h264-only",
		},
	})
}

func testTrackCompositeFile(t *testing.T, conf *config.Config, room *lksdk.Room, audioCodec, videoCodec string, cases []*testCase) {
	p := publishSamplesToRoom(t, room, audioCodec, videoCodec)

	for _, test := range cases {
		if !t.Run(test.name, func(t *testing.T) {
			runTrackCompositeFileTest(t, conf, p, test)
		}) {
			t.FailNow()
		}
	}

	require.NoError(t, room.LocalParticipant.UnpublishTrack(p.audioTrackID))
	require.NoError(t, room.LocalParticipant.UnpublishTrack(p.videoTrackID))
}

func runTrackCompositeFileTest(t *testing.T, conf *config.Config, params *sdkParams, test *testCase) {
	filepath, filename := getFileInfo(conf, test, "tc")

	var audioTrackID, videoTrackID string
	if !test.videoOnly {
		audioTrackID = params.audioTrackID
	}
	if !test.audioOnly {
		videoTrackID = params.videoTrackID
	}

	trackRequest := &livekit.TrackCompositeEgressRequest{
		RoomName:     params.roomName,
		AudioTrackId: audioTrackID,
		VideoTrackId: videoTrackID,
		Output: &livekit.TrackCompositeEgressRequest_File{
			File: &livekit.EncodedFileOutput{
				FileType: test.fileType,
				Filepath: filepath,
			},
		},
	}

	if test.options != nil {
		trackRequest.Options = &livekit.TrackCompositeEgressRequest_Advanced{
			Advanced: test.options,
		}
	}

	req := &livekit.StartEgressRequest{
		EgressId:  utils.NewGuid(utils.EgressPrefix),
		RequestId: utils.NewGuid(utils.RPCPrefix),
		SentAt:    time.Now().UnixNano(),
		Request: &livekit.StartEgressRequest_TrackComposite{
			TrackComposite: trackRequest,
		},
	}

	runFileTest(t, conf, test, req, filename)
}

func testTrack(t *testing.T, conf *config.Config, room *lksdk.Room) {
	for _, test := range []*testCase{
		{
			audioOnly:     true,
			codec:         "opus",
			fileExtension: "ogg",
		},
		{

			videoOnly:     true,
			codec:         "vp8",
			fileExtension: "ivf",
		},
		{
			videoOnly:     true,
			codec:         "h264",
			fileExtension: "h264",
		},
	} {
		test.filePrefix = fmt.Sprintf("track-%s", test.codec)

		if !t.Run(test.filePrefix, func(t *testing.T) {
			runTrackFileTest(t, conf, room, test)
		}) {
			// t.FailNow()
		}
	}
}

func runTrackFileTest(t *testing.T, conf *config.Config, room *lksdk.Room, test *testCase) {
	var trackID string
	if test.audioOnly {
		p := publishSamplesToRoom(t, room, test.codec, "")
		trackID = p.audioTrackID
	} else {
		p := publishSamplesToRoom(t, room, "", test.codec)
		trackID = p.videoTrackID
	}

	time.Sleep(time.Second * 5)

	filepath, filename := getFileInfo(conf, test, "track")

	trackRequest := &livekit.TrackEgressRequest{
		RoomName: room.Name,
		TrackId:  trackID,
		Output: &livekit.TrackEgressRequest_File{
			File: &livekit.DirectFileOutput{
				Filepath: filepath,
			},
		},
	}

	req := &livekit.StartEgressRequest{
		EgressId:  utils.NewGuid(utils.EgressPrefix),
		RequestId: utils.NewGuid(utils.RPCPrefix),
		SentAt:    time.Now().UnixNano(),
		Request: &livekit.StartEgressRequest_Track{
			Track: trackRequest,
		},
	}

	runFileTest(t, conf, test, req, filename)

	require.NoError(t, room.LocalParticipant.UnpublishTrack(trackID))
}

func testTrackWs(t *testing.T, conf *config.Config, room *lksdk.Room) {
	for _, test := range []*testCase{
		{
			audioOnly:     true,
			codec:         "opus",
			fileExtension: "ogg",
		},
		{

			videoOnly:     true,
			codec:         "vp8",
			fileExtension: "ivf",
		},
		{
			videoOnly:     true,
			codec:         "h264",
			fileExtension: "h264",
		},
	} {
		test.filePrefix = fmt.Sprintf("track_ws-%s", test.codec)

		if !t.Run(test.filePrefix, func(t *testing.T) {
			runTrackWsTest(t, conf, room, test)
		}) {
			// t.FailNow()
		}
	}
}

type testWebsocketServer struct {
	port     string
	conn     *websocket.Conn
	file     *os.File
	done     chan struct{}
	upgrader websocket.Upgrader
}

func (s *testWebsocketServer) handleWS(w http.ResponseWriter, r *http.Request) {
	// Determine file type
	ct := r.Header.Get("Content-Type")
	var err error
	switch {
	case strings.EqualFold(ct, "video/vp8"):
		s.file, err = os.Create(shortuuid.New() + ".ivf")
	case strings.EqualFold(ct, "video/h264"):
		s.file, err = os.Create(shortuuid.New() + ".h264")
	case strings.EqualFold(ct, "audio/opus"):
		s.file, err = os.Create(shortuuid.New() + ".ogg")
	default:
		log.Fatal("Unsupported codec")
		return
	}
	if err != nil {
		log.Fatalf("Error in creating file: %s\n", err)
		return
	}

	// Try accepting the WS connection
	s.conn, err = s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatalf("Error in accepting WS connection: %s\n", err)
		return
	}

	go s.writeToFile()
}

func (s *testWebsocketServer) writeToFile() {
	defer s.close()

	for {
		select {
		case <-s.done:
			return
		default:
			mt, msg, err := s.conn.ReadMessage()
			if err != nil {
				log.Fatalf("Error in reading message: %s\n", err)
				return
			}

			switch mt {
			case websocket.BinaryMessage:
				if s.file == nil {
					log.Fatal("File is not open")
					return
				}
				_, err = s.file.Write(msg)
				if err != nil {
					log.Fatalf("Error while writing to file: %s\n", err)
					return
				}
			}
		}
	}
}

func (s *testWebsocketServer) close() {
	if s.conn != nil {
		s.conn.Close()
	}
	if s.file != nil {
		s.file.Close()
	}
}

func (s *testWebsocketServer) Start() {
	http.ListenAndServe(":"+s.port, nil)
}

func (s *testWebsocketServer) Stop() {
	select {
	case <-s.done:
		return
	default:
		close(s.done)
	}
}

func createWebsocketServer(port string) *testWebsocketServer {
	srv := &testWebsocketServer{
		port:     port,
		done:     make(chan struct{}),
		upgrader: websocket.Upgrader{},
	}
	http.HandleFunc("/ws", srv.handleWS)
	return srv
}

func runTrackWsTest(t *testing.T, conf *config.Config, room *lksdk.Room, test *testCase) {

	var nodeIP, port string
	fmt.Sscanf(conf.WsUrl, "ws://%s:%s", &nodeIP, &port)

	// Start websocket server
	srv := createWebsocketServer("9700")
	go srv.Start()

	var trackID string
	if test.audioOnly {
		p := publishSamplesToRoom(t, room, test.codec, "")
		trackID = p.audioTrackID
	} else {
		p := publishSamplesToRoom(t, room, "", test.codec)
		trackID = p.videoTrackID
	}

	time.Sleep(time.Second * 5)

	_, filename := getFileInfo(conf, test, "track_ws")

	trackRequest := &livekit.TrackEgressRequest{
		RoomName: room.Name,
		TrackId:  trackID,
		Output: &livekit.TrackEgressRequest_WebsocketUrl{
			WebsocketUrl: fmt.Sprintf("ws://%s:9700", nodeIP),
		},
	}

	req := &livekit.StartEgressRequest{
		EgressId:  utils.NewGuid(utils.EgressPrefix),
		RequestId: utils.NewGuid(utils.RPCPrefix),
		SentAt:    time.Now().UnixNano(),
		Request: &livekit.StartEgressRequest_Track{
			Track: trackRequest,
		},
	}

	time.Sleep(time.Second * 15)
	srv.Stop()

	runFileTest(t, conf, test, req, filename)

	require.NoError(t, room.LocalParticipant.UnpublishTrack(trackID))
}
