import EgressHelper from '@livekit/egress-sdk';
import { AudioRenderer, useRoom } from '@livekit/react-components';
import {
  AudioTrack, Participant, RemoteParticipant, Room, RoomEvent,
} from 'livekit-client';
import {
  ReactElement, useCallback, useEffect, useState,
} from 'react';
import GridLayout from './GridLayout';
import SingleSpeakerLayout from './SingleSpeakerLayout';
import SpeakerLayout from './SpeakerLayout';

interface RoomPageProps {
  url: string;
  token: string;
  layout: string;
}

export default function RoomPage({ url, token, layout: initialLayout }: RoomPageProps) {
  const [layout, setLayout] = useState(initialLayout);
  const roomState = useRoom({
    adaptiveStream: true,
  });
  const { room, participants, audioTracks } = roomState;

  useEffect(() => {
    roomState.connect(url, token);
  }, [url]);

  useEffect(() => {
    if (room) {
      EgressHelper.setRoom(room, {
        autoEnd: true,
      });
      // Egress layout can change on the fly, we can react to the new layout
      // here.
      EgressHelper.onLayoutChanged((newLayout) => {
        setLayout(newLayout);
      });

      // start recording immediately after connection
      EgressHelper.startRecording();
    }
  }, [room]);

  if (!url || !token) {
    return <div className="error">missing required params url and token</div>;
  }

  // not ready yet, don't render anything
  if (!room) {
    return <div />;
  }

  // filter out local participant
  const remoteParticipants = participants.filter((p) => p instanceof RemoteParticipant);

  return (
    <Stage
      layout={layout}
      room={room}
      participants={remoteParticipants}
      audioTracks={audioTracks}
    />
  );
}

interface StageProps {
  layout: string;
  room: Room;
  participants: Participant[];
  audioTracks: AudioTrack[];
}

function Stage({
  layout, room, participants, audioTracks,
}: StageProps) {
  const [hasScreenShare, setHasScreenShare] = useState(false);

  const onTrackChanged = useCallback(() => {
    let foundScreenshare = false;
    room.participants.forEach((p) => {
      if (p.isScreenShareEnabled) {
        foundScreenshare = true;
      }
    });
    setHasScreenShare(foundScreenshare);
  }, [room]);

  useEffect(() => {
    if (!room) {
      return;
    }
    room.on(RoomEvent.TrackPublished, onTrackChanged);
    room.on(RoomEvent.TrackUnpublished, onTrackChanged);
    room.on(RoomEvent.ConnectionStateChanged, onTrackChanged);
    return () => {
      room.off(RoomEvent.TrackPublished, onTrackChanged);
      room.off(RoomEvent.TrackUnpublished, onTrackChanged);
      room.off(RoomEvent.ConnectionStateChanged, onTrackChanged);
    };
  }, [room]);

  let interfaceStyle = 'dark';
  if (layout.endsWith('-light')) {
    interfaceStyle = 'light';
  }

  let containerClass = 'roomContainer';
  if (interfaceStyle) {
    containerClass += ` ${interfaceStyle}`;
  }

  // determine layout to use
  let main: ReactElement = <></>;
  if (hasScreenShare && layout.startsWith('grid')) {
    layout = layout.replace('grid', 'speaker');
  }
  if (layout.startsWith('speaker')) {
    main = (
      <SpeakerLayout
        room={room}
        participants={participants}
      />
    );
  } else if (layout.startsWith('single-speaker')) {
    main = (
      <SingleSpeakerLayout
        room={room}
        participants={participants}
      />
    );
  } else if (layout.startsWith('grid')) {
    main = (
      <GridLayout
        room={room}
        participants={participants}
      />
    );
  }

  return (
    <div className={containerClass}>
      {main}
      {audioTracks.map((track) => (
        <AudioRenderer key={track.sid} track={track} isLocal={false} />
      ))}
    </div>
  );
}
