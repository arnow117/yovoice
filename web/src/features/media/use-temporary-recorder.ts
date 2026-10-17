import { useCallback, useEffect, useRef, useState } from 'react';

type RecordingSnapshot = {
  active: boolean;
  recording: boolean;
  startedAt: number | null;
  stream: MediaStream | null;
};

type RecordingCompletion = {
  autoStopped: boolean;
  blob: Blob;
  durationSeconds: number;
  isCurrent: () => boolean;
};

type RecorderCallbacks = {
  onComplete?: (completion: RecordingCompletion) => void | Promise<void>;
  onError?: (error: unknown) => void;
  onState?: (snapshot: RecordingSnapshot) => void;
};

export type TemporaryRecorderDependencies = {
  createRecorder: (stream: MediaStream) => MediaRecorder;
  now: () => number;
  requestStream: () => Promise<MediaStream>;
  clearTimer: (timer: ReturnType<typeof setTimeout>) => void;
  setTimer: (callback: () => void, durationMs: number) => ReturnType<typeof setTimeout>;
};

type RecorderSession = {
  chunks: Blob[];
  id: number;
  autoStopped: boolean;
  recorder: MediaRecorder | null;
  startedAt: number | null;
  stream: MediaStream | null;
  timer: ReturnType<typeof setTimeout> | null;
};

const idleSnapshot: RecordingSnapshot = {
  active: false,
  recording: false,
  startedAt: null,
  stream: null,
};

const browserDependencies: TemporaryRecorderDependencies = {
  createRecorder: stream => new MediaRecorder(stream),
  now: () => performance.now(),
  requestStream: () => navigator.mediaDevices.getUserMedia({ audio: true }),
  clearTimer: timer => clearTimeout(timer),
  setTimer: (callback, durationMs) => setTimeout(callback, durationMs),
};

function stopTracks(stream: MediaStream | null) {
  stream?.getTracks().forEach(track => track.stop());
}

/**
 * 临时录音控制器将每次授权、录音及结束事件绑定到递增身份。换输入、关闭页面或重录后，
 * 旧 getUserMedia/onstop 都只能释放自身资源，不能写回新的表单状态。
 */
export function createTemporaryRecorderController(callbacks: RecorderCallbacks, dependencies: TemporaryRecorderDependencies = browserDependencies, maxDurationMs = 60_000) {
  let current: RecorderSession | null = null;
  let sequence = 0;
  let destroyed = false;

  function emit() {
    if (!current) {
      callbacks.onState?.(idleSnapshot);
      return;
    }
    callbacks.onState?.({
      active: true,
      recording: current.recorder?.state === 'recording',
      startedAt: current.recorder?.state === 'recording' ? current.startedAt : null,
      stream: current.stream,
    });
  }

  function valid(session: RecorderSession) {
    return !destroyed && current === session;
  }

  function clearSessionTimer(session: RecorderSession) {
    if (!session.timer) return;
    dependencies.clearTimer(session.timer);
    session.timer = null;
  }

  function discard() {
    const session = current;
    sequence += 1;
    current = null;
    if (session) {
      clearSessionTimer(session);
      if (session.recorder) {
        session.recorder.ondataavailable = null;
        session.recorder.onstop = null;
        if (session.recorder.state === 'recording') session.recorder.stop();
      }
      stopTracks(session.stream);
    }
    emit();
  }

  function finish(session: RecorderSession) {
    if (!valid(session)) {
      clearSessionTimer(session);
      stopTracks(session.stream);
      return;
    }
    clearSessionTimer(session);
    current = null;
    stopTracks(session.stream);
    emit();
    const durationSeconds = (dependencies.now() - (session.startedAt ?? dependencies.now())) / 1000;
    const isCurrent = () => !destroyed && sequence === session.id;
    void callbacks.onComplete?.({
      autoStopped: session.autoStopped,
      blob: new Blob(session.chunks, { type: session.recorder?.mimeType || 'audio/webm' }),
      durationSeconds,
      isCurrent,
    });
  }

  function fail(session: RecorderSession, error: unknown) {
    if (!valid(session)) {
      stopTracks(session.stream);
      return;
    }
    clearSessionTimer(session);
    current = null;
    stopTracks(session.stream);
    emit();
    callbacks.onError?.(error);
  }

  function stopSession(session: RecorderSession) {
    if (!valid(session)) return;
    clearSessionTimer(session);
    if (!session.recorder || session.recorder.state !== 'recording') {
      discard();
      return;
    }
    session.recorder.stop();
  }

  function stop() {
    if (current) stopSession(current);
  }

  async function start() {
    discard();
    const session: RecorderSession = {
      chunks: [],
      id: ++sequence,
      autoStopped: false,
      recorder: null,
      startedAt: null,
      stream: null,
      timer: null,
    };
    current = session;
    emit();
    try {
      const stream = await dependencies.requestStream();
      if (!valid(session)) {
        stopTracks(stream);
        return;
      }
      session.stream = stream;
      const recorder = dependencies.createRecorder(stream);
      session.recorder = recorder;
      recorder.ondataavailable = event => {
        if (valid(session) && event.data.size) session.chunks.push(event.data);
      };
      recorder.onstop = () => finish(session);
      session.startedAt = dependencies.now();
      recorder.start();
      session.timer = dependencies.setTimer(() => {
        if (!valid(session) || session.recorder?.state !== 'recording') return;
        session.autoStopped = true;
        stopSession(session);
      }, maxDurationMs);
      emit();
    } catch (error) {
      fail(session, error);
    }
  }

  return {
    activate() {
      // React StrictMode deliberately runs an effect cleanup before mounting the
      // effect again in development. The cleanup has already invalidated every
      // prior session, so the next effect setup can safely accept new sessions.
      destroyed = false;
    },
    destroy() {
      destroyed = true;
      discard();
    },
    discard,
    start,
    stop,
  };
}

export function useTemporaryRecorder(callbacks: Omit<RecorderCallbacks, 'onState'>, maxDurationMs = 60_000) {
  const callbacksRef = useRef(callbacks);
  callbacksRef.current = callbacks;
  const [snapshot, setSnapshot] = useState<RecordingSnapshot>(idleSnapshot);
  const controllerRef = useRef<ReturnType<typeof createTemporaryRecorderController> | null>(null);
  if (!controllerRef.current) {
    controllerRef.current = createTemporaryRecorderController({
      onComplete: completion => callbacksRef.current.onComplete?.(completion),
      onError: error => callbacksRef.current.onError?.(error),
      onState: setSnapshot,
    }, browserDependencies, maxDurationMs);
  }
  const controller = controllerRef.current;

  useEffect(() => {
    controller.activate();
    return () => controller.destroy();
  }, [controller]);

  return {
    ...snapshot,
    discard: useCallback(() => controller.discard(), [controller]),
    start: useCallback(() => controller.start(), [controller]),
    stop: useCallback(() => controller.stop(), [controller]),
  };
}

export async function decodeTemporaryRecording(blob: Blob, createContext: () => AudioContext = () => new AudioContext()) {
  const context = createContext();
  try {
    return await context.decodeAudioData(await blob.arrayBuffer());
  } finally {
    await context.close();
  }
}
