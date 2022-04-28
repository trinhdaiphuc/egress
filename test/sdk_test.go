//go:build integration
// +build integration

package test

import (
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
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
		//{
		//
		//	videoOnly:     true,
		//	codec:         "vp8",
		//	fileExtension: "ivf",
		//},
		//{
		//	videoOnly:     true,
		//	codec:         "h264",
		//	fileExtension: "h264",
		//},
	} {
		test.filePrefix = fmt.Sprintf("track_ws-%s", test.codec)

		if !t.Run(test.filePrefix, func(t *testing.T) {
			runTrackWsTest(t, conf, room, test)
		}) {
			// t.FailNow()
		}
	}
}

var done = make(chan struct{})
var wsFilepath = ""

func handleWebsocket(w http.ResponseWriter, r *http.Request) {
	// Determine file type
	ct := r.Header.Get("Content-Type")

	var err error
	var file *os.File
	var conn *websocket.Conn
	var upgrader = websocket.Upgrader{}

	switch {
	case strings.EqualFold(ct, "video/vp8"):
		file, err = os.Create(wsFilepath)
	case strings.EqualFold(ct, "video/h264"):
		file, err = os.Create(wsFilepath)
	case strings.EqualFold(ct, "audio/opus"):
		file, err = os.Create(wsFilepath)
	default:
		log.Fatal("Unsupported codec")
		return
	}
	if err != nil {
		log.Fatalf("Error in creating file: %s\n", err)
		return
	}

	// Try accepting the WS connection
	conn, err = upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatalf("Error in accepting WS connection: %s\n", err)
		return
	}

	log.Println("Websocket connection received!")

	go func() {
		defer func() {
			conn.Close()
			file.Close()
		}()

		for {
			select {
			case <-done:
				return
			default:
				mt, msg, err := conn.ReadMessage()
				if err != nil {
					log.Fatalf("Error in reading message: %s\n", err)
					return
				}

				switch mt {
				case websocket.BinaryMessage:
					if file == nil {
						log.Fatal("File is not open")
						return
					}
					_, err = file.Write(msg)
					if err != nil {
						log.Fatalf("Error while writing to file: %s\n", err)
						return
					}
				}
			}
		}
	}()
}

func runTrackWsTest(t *testing.T, conf *config.Config, room *lksdk.Room, test *testCase) {
	s := httptest.NewServer(http.HandlerFunc(handleWebsocket))
	defer func() {
		close(done)
		s.Close()
	}()

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
	wsFilepath = filename

	trackRequest := &livekit.TrackEgressRequest{
		RoomName: room.Name,
		TrackId:  trackID,
		Output: &livekit.TrackEgressRequest_WebsocketUrl{
			WebsocketUrl: "ws" + strings.TrimPrefix(s.URL, "http"),
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
