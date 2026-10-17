import { expect, test } from '@playwright/test';
import { createTemporaryRecorderController, decodeTemporaryRecording, type TemporaryRecorderDependencies } from '../src/features/media/use-temporary-recorder';

class FakeRecorder {
  mimeType = 'audio/webm';
  ondataavailable: ((event: BlobEvent) => void) | null = null;
  onstop: ((event: Event) => void) | null = null;
  state: RecordingState = 'inactive';

  start() {
    this.state = 'recording';
  }

  stop() {
    this.state = 'inactive';
    this.onstop?.(new Event('stop'));
  }

  data(blob: Blob) {
    this.ondataavailable?.({ data: blob } as BlobEvent);
  }
}

class DeferredStopRecorder extends FakeRecorder {
  stop() {
    this.state = 'inactive';
  }

  finishStop() {
    this.onstop?.(new Event('stop'));
  }
}

function fakeStream() {
  const track = { stopped: false, stop() { this.stopped = true; } };
  const stream = {
    getAudioTracks: () => [track],
    getTracks: () => [track],
  } as unknown as MediaStream;
  return { stream, track };
}

function recorderDependencies(streams: MediaStream[]) {
  const recorders: FakeRecorder[] = [];
  const timers = new Map<number, () => void>();
  let nextTimer = 0;
  let now = 0;
  const dependencies: TemporaryRecorderDependencies = {
    createRecorder: () => {
      const recorder = new FakeRecorder();
      recorders.push(recorder);
      return recorder as unknown as MediaRecorder;
    },
    now: () => now,
    requestStream: async () => streams.shift()!,
    clearTimer: timer => timers.delete(timer as unknown as number),
    setTimer: callback => {
      const id = ++nextTimer;
      timers.set(id, callback);
      return id as unknown as ReturnType<typeof setTimeout>;
    },
  };
  return { dependencies, recorders, setNow: (value: number) => { now = value; }, timers };
}

test('临时录音延迟授权、旧 onstop 和旧定时器均不能写回新会话', async () => {
  const first = fakeStream();
  let resolveFirst: ((stream: MediaStream) => void) | undefined;
  const firstPromise = new Promise<MediaStream>(resolve => { resolveFirst = resolve; });
  const stoppedTracks: boolean[] = [];
  const controller = createTemporaryRecorderController({
    onComplete: ({ isCurrent }) => { stoppedTracks.push(isCurrent()); },
  }, {
    createRecorder: _stream => new FakeRecorder() as unknown as MediaRecorder,
    now: () => 0,
    requestStream: () => firstPromise,
    clearTimer: () => {},
    setTimer: () => 0 as unknown as ReturnType<typeof setTimeout>,
  });
  const pendingPermission = controller.start();
  controller.discard();
  resolveFirst!(first.stream);
  await pendingPermission;
  expect(first.track.stopped).toBe(true);
  expect(stoppedTracks).toEqual([]);

  const second = fakeStream();
  const third = fakeStream();
  const fixture = recorderDependencies([second.stream, third.stream]);
  const completed: string[] = [];
  const protectedController = createTemporaryRecorderController({
    onComplete: ({ blob, isCurrent }) => { if (isCurrent()) completed.push(blob.type); },
  }, fixture.dependencies);
  await protectedController.start();
  const oldStop = fixture.recorders[0].onstop!;
  const oldTimer = fixture.timers.get(1)!;
  protectedController.discard();
  await protectedController.start();
  fixture.recorders[1].data(new Blob(['new'], { type: 'audio/wav' }));
  oldStop(new Event('stop'));
  expect(fixture.recorders[1].state).toBe('recording');
  oldTimer();
  expect(fixture.recorders[1].state).toBe('recording');
  fixture.timers.get(2)!();
  expect(fixture.recorders[1].state).toBe('inactive');
  expect(second.track.stopped).toBe(true);
  expect(third.track.stopped).toBe(true);
  expect(completed).toEqual(['audio/webm']);
});

test('临时录音把自动停止与手动停止传给当前会话，并释放两次采集资源', async () => {
  const automatic = fakeStream();
  const manual = fakeStream();
  const fixture = recorderDependencies([automatic.stream, manual.stream]);
  const completions: Array<{ autoStopped: boolean; durationSeconds: number }> = [];
  const controller = createTemporaryRecorderController({
    onComplete: ({ autoStopped, durationSeconds }) => {
      completions.push({ autoStopped, durationSeconds });
    },
  }, fixture.dependencies);

  await controller.start();
  fixture.setNow(60_000);
  fixture.timers.get(1)!();

  await controller.start();
  fixture.setNow(65_000);
  controller.stop();

  expect(completions).toEqual([
    { autoStopped: true, durationSeconds: 60 },
    { autoStopped: false, durationSeconds: 5 },
  ]);
  expect(automatic.track.stopped).toBe(true);
  expect(manual.track.stopped).toBe(true);
});

test('手动停止后的迟到计时器不污染自动停止标记或丢失完成回调', async () => {
  const source = fakeStream();
  const timers = new Map<number, () => void>();
  const recorder = new DeferredStopRecorder();
  const completions: boolean[] = [];
  const controller = createTemporaryRecorderController({
    onComplete: ({ autoStopped }) => { completions.push(autoStopped); },
  }, {
    createRecorder: () => recorder as unknown as MediaRecorder,
    now: () => 59_990,
    requestStream: async () => source.stream,
    clearTimer: timer => timers.delete(timer as unknown as number),
    setTimer: callback => {
      timers.set(1, callback);
      return 1 as unknown as ReturnType<typeof setTimeout>;
    },
  });

  await controller.start();
  const lateTimer = timers.get(1)!;
  controller.stop();
  lateTimer();
  recorder.finishStop();

  expect(completions).toEqual([false]);
  expect(source.track.stopped).toBe(true);
});

test('临时录音解码无论成功或失败都会关闭 AudioContext', async () => {
  let closed = 0;
  const context = {
    close: async () => { closed += 1; },
    decodeAudioData: async () => { throw new Error('decode failed'); },
  } as unknown as AudioContext;
  await expect(decodeTemporaryRecording(new Blob(['invalid']), () => context)).rejects.toThrow('decode failed');
  expect(closed).toBe(1);
});

test('临时录音控制器可在 StrictMode 效果清理后重新接受新会话', async () => {
  const source = fakeStream();
  const fixture = recorderDependencies([source.stream]);
  const controller = createTemporaryRecorderController({}, fixture.dependencies);

  controller.destroy();
  controller.activate();
  await controller.start();

  expect(fixture.recorders).toHaveLength(1);
  expect(fixture.recorders[0].state).toBe('recording');
});
