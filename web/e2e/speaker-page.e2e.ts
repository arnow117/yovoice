import { test, type Browser } from '@e2e-dev/web';
import { expect, type Screen } from 'e2e';

const musicSourceID = '00000000000000000000000000000011';
const scoreSourceID = '00000000000000000000000000000022';
const textSourceID = '00000000000000000000000000000033';

type TemporaryAudioProbe = {
  created: string[];
  revoked: string[];
  originalCreate: typeof URL.createObjectURL;
  originalRevoke: typeof URL.revokeObjectURL;
};

declare global {
  interface Window {
    speakerTemporaryAudioProbe?: TemporaryAudioProbe;
  }
}

async function enrollBoundProfile(screen: Screen) {
  await screen.getByTestId('speaker-enroll-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await screen.getByTestId('speaker-profile-name').fill('Fixture A');
  await screen.getByTestId('speaker-profile-bind-voice').tap();
  await screen.getByRole('option', 'Bound voice').tap();
  await screen.getByTestId('speaker-enroll').tap();
  await expect(screen.getByTestId('speaker-profile-00000000000000000000000000000001')).toBeVisible();
}

async function selectGenerationInputs(screen: Screen, modelName: string) {
  await screen.getByTestId('speaker-generation-profile').tap();
  await screen.getByRole('option', /Fixture A/).tap();
  await screen.getByTestId('speaker-generation-model').tap();
  await screen.getByRole('option', modelName).tap();
}

async function configureFixture(browser: { evaluate: <T, A>(pageFunction: (arg: A) => T | Promise<T>, arg: A) => Promise<T> }, controls: Record<string, unknown>) {
  await browser.evaluate(next => window.__yovoiceSyntheticSpeakerFixture.configure(next), controls);
}

async function waitFor(browser: { evaluate: <T>(pageFunction: () => T | Promise<T>) => Promise<T> }, durationMs: number) {
  await browser.evaluate(duration => new Promise(resolve => setTimeout(resolve, duration)), durationMs);
}

async function waitForTemporaryAudioProbe(browser: Browser, expected: { created: number; revoked: number }) {
  return browser.evaluate(async counts => {
    const deadline = performance.now() + 2_000;
    while (performance.now() < deadline) {
      const probe = window.speakerTemporaryAudioProbe;
      if (probe && probe.created.length >= counts.created && probe.revoked.length >= counts.revoked) {
        return { created: [...probe.created], revoked: [...probe.revoked] };
      }
      await new Promise(resolve => setTimeout(resolve, 10));
    }
    throw new Error(`temporary audio probe did not reach ${counts.created} created and ${counts.revoked} revoked URLs`);
  }, expected);
}

async function waitForHeldListResponse(browser: Browser) {
  return browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.waitForHeldListResponse());
}

async function releaseHeldListResponse(browser: Browser, id: string) {
  return browser.evaluate(responseID => window.__yovoiceSyntheticSpeakerFixture.releaseHeldListResponse(responseID), id);
}

async function waitForHeldEnrollResponse(browser: Browser) {
  return browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.waitForHeldEnrollResponse());
}

async function releaseHeldEnrollResponse(browser: Browser, id: string) {
  return browser.evaluate(responseID => window.__yovoiceSyntheticSpeakerFixture.releaseHeldEnrollResponse(responseID), id);
}

async function openSpeakerPage(app: { open: () => Promise<void> }, screen: Screen) {
  await app.open();
  await screen.getByTestId('nav-speakers').tap();
}

async function serveSyntheticGeneratedOutput(browser: Browser) {
  const fulfillAudio = () => ({
    path: 'e2e/fixtures/generated-output.wav',
    contentType: 'audio/wav',
  });
  await browser.route('https://outputs.workbench.local/**', route => route.fulfill(fulfillAudio()));
  await browser.route('https://voices.workbench.local/**', route => route.fulfill(fulfillAudio()));
}

async function generationCallCount(browser: Browser) {
  return browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.calls
    .filter(call => call.method === 'speaker.generate').length);
}

interface GenerationExpectation {
  previousCallCount: number;
  modelID: string;
  sourceID?: string;
}

async function latestGenerationSnapshot(browser: Browser, expectation: GenerationExpectation) {
  return browser.evaluate(async expected => {
    const deadline = performance.now() + 2_000;
    while (performance.now() < deadline) {
      const fixture = window.__yovoiceSyntheticSpeakerFixture;
      const calls = fixture.calls.filter(call => call.method === 'speaker.generate');
      if (calls.length > expected.previousCallCount) {
        if (calls.length !== expected.previousCallCount + 1) {
          throw new Error(`speaker.generate added ${calls.length - expected.previousCallCount} requests for one click`);
        }

        const request = calls.at(-1);
        const priorRequestIDs = new Set(calls
          .slice(0, expected.previousCallCount)
          .map(call => call.data?.requestId));
        const requestID = request?.data?.requestId;
        const draft = request.data?.draft;
        if (!draft || typeof draft !== 'object') {
          throw new Error('speaker.generate request did not include a draft');
        }
        if (typeof requestID !== 'string' || !/^[a-f0-9]{32}$/.test(requestID) || priorRequestIDs.has(requestID)) {
          throw new Error('speaker.generate did not create a fresh request ID');
        }
        if (draft.modelId !== expected.modelID) {
          throw new Error(`speaker.generate model was ${String(draft.modelId)}, expected ${expected.modelID}`);
        }

        const source = expected.sourceID
          ? fixture.state.drafts.find(item => item.id === expected.sourceID) ?? null
          : null;
        return JSON.parse(JSON.stringify({
          requestId: requestID,
          source,
          draft,
          callCount: calls.length,
        }));
      }
      await new Promise(resolve => setTimeout(resolve, 25));
    }
    throw new Error('speaker.generate request was not captured after the click');
  }, expectation);
}

async function mockRecording(browser: Browser) {
  await browser.evaluate(() => {
    class FixtureMediaRecorder {
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
    }

    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: {
        getUserMedia: async () => ({
          getTracks: () => [{ stop: () => undefined }],
        }),
      },
    });
    Object.defineProperty(window, 'MediaRecorder', {
      configurable: true,
      value: FixtureMediaRecorder,
    });
  });
}

function cancelButton(screen: Screen) {
  return screen.getByRole('button', /取消操作|Cancel operation/);
}

test('合成桌面桥接下的声纹页保持显式输入与独立档案', async ({ app, browser, screen }) => {
  await app.open();
  await screen.getByTestId('nav-speakers').tap();
  await expect(screen.getByTestId('speaker-enroll')).toBeDisabled({ timeout: 10_000 });

  await screen.getByTestId('speaker-enroll-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await screen.getByTestId('speaker-profile-name').fill('Fixture A');
  await expect(screen.getByTestId('speaker-enroll')).toBeEnabled();
  await screen.getByTestId('speaker-enroll').tap();
  await expect(screen.getByTestId('speaker-profile-00000000000000000000000000000001')).toBeVisible();

  await screen.getByTestId('speaker-enroll-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await screen.getByTestId('speaker-profile-name').fill('Fixture B');
  await screen.getByTestId('speaker-enroll').tap();
  await expect(screen.getByTestId('speaker-profile-00000000000000000000000000000002')).toBeVisible();

  await screen.getByTestId('speaker-identify-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await screen.getByTestId('speaker-identify').tap();
  await expect(screen.getByTestId('speaker-result')).toHaveAttribute('data-status', 'matched');

  const speakerCalls = await browser.evaluate(() => (window.__yovoiceSyntheticSpeakerFixture?.calls ?? []).filter(call => call.method.startsWith('speaker.')).map(call => call.method));
  expect(speakerCalls).toContain('speaker.enroll');
  expect(speakerCalls).toContain('speaker.identify');
  expect(speakerCalls).not.toContain('speaker.generate');
  await app.screenshot('synthetic-speaker-page');
});

test('进入声纹页再返回原作品时保留正文与原项目', async ({ app, browser, screen }) => {
  await app.open();
  await browser.evaluate(id => localStorage.setItem('yovoice-active-project', id), textSourceID);
  await browser.reload();
  const sourceBefore = await browser.evaluate(id => JSON.stringify(window.__yovoiceSyntheticSpeakerFixture.state.drafts.find(item => item.id === id)), textSourceID);

  await screen.getByTestId('nav-speakers').tap();
  await expect(screen.getByTestId('speaker-enroll')).toBeVisible();
  await screen.getByTestId('nav-text').tap();
  await screen.getByRole('button', 'Synthetic text source').first().tap();

  const editor = await browser.evaluate(() => {
    const element = document.querySelector<HTMLTextAreaElement>('.script-editor');
    if (!element) return null;
    const style = getComputedStyle(element);
    return {
      text: element.value,
      visible: style.display !== 'none' && style.visibility !== 'hidden',
    };
  });
  const sourceAfter = await browser.evaluate(id => JSON.stringify(window.__yovoiceSyntheticSpeakerFixture.state.drafts.find(item => item.id === id)), textSourceID);
  expect(editor).toEqual({ text: 'A normal text project.', visible: true });
  expect(sourceAfter).toBe(sourceBefore);
});

test('音乐和编曲作品的声纹生成发送独立的 TTS 草稿', async ({ app, browser, screen }) => {
  await serveSyntheticGeneratedOutput(browser);
  const sources = [
    {
      id: musicSourceID,
      kind: 'music',
      modelName: 'Qwen3-TTS 1.7B Base',
      modelId: 'qwen3-tts-base-q8',
      options: { text_chunk_size: 256, text_chunk_mode: 'tag_aware' },
    },
    {
      id: scoreSourceID,
      kind: 'score',
      modelName: 'Index TTS 2.5',
      modelId: 'index-2.5-q8',
      options: { text_chunk_size: 384, text_chunk_mode: 'endline' },
    },
  ];

  for (const source of sources) {
    await app.open();
    await browser.evaluate(id => localStorage.setItem('yovoice-active-project', id), source.id);
    await browser.reload();
    await screen.getByTestId('nav-speakers').tap();
    const originalSource = await browser.evaluate(id => JSON.stringify(window.__yovoiceSyntheticSpeakerFixture.state.drafts.find(item => item.id === id)), source.id);
    await enrollBoundProfile(screen);
    await selectGenerationInputs(screen, source.modelName);
    await browser.locator('#speaker-generation-text').fill(`TTS output for ${source.id}`);
    const previousCallCount = await generationCallCount(browser);
    await screen.getByTestId('speaker-generate').tap();

    const request = await latestGenerationSnapshot(browser, {
      previousCallCount,
      modelID: source.modelId,
      sourceID: source.id,
    });

    expect(request.requestId).toMatch(/^[a-f0-9]{32}$/);
    expect(request.source?.id).toBe(source.id);
    expect(request.source?.kind).toBe(source.kind);
    expect(JSON.stringify(request.source)).toBe(originalSource);
    expect(request.draft.kind).toBe('text');
    expect(request.draft.id === source.id).toBe(false);
    expect(Object.hasOwn(request.draft, 'score')).toBe(false);
    expect(Object.hasOwn(request.draft, 'subtitles')).toBe(false);
    expect(Object.hasOwn(request.draft, 'timeline')).toBe(false);
    expect(Object.hasOwn(request.draft, 'lyrics')).toBe(false);
    expect(Object.hasOwn(request.draft, 'instrumental')).toBe(false);
    expect(request.draft.synthesisLanguage).toBe('auto');
    expect(request.draft.modelId).toBe(source.modelId);
    expect(request.draft.modelOptions).toEqual({ index_tts2: source.options });
    expect(request.draft.mode).toBe('text');
    expect(request.draft.language).toBe('zh');
    expect(request.draft.speed).toBe(1);
    expect(request.draft.emotionStrength).toBe(0);
    expect(request.draft.intervalSilenceMs).toBe(0);
    expect(request.draft.emotions).toEqual([0, 0, 0, 0, 0, 0, 0, 0.5]);
  }
});

test('普通文本作品的声纹生成保留合法的 TTS 设置和作品关联', async ({ app, browser, screen }) => {
  await serveSyntheticGeneratedOutput(browser);
  await app.open();
  await browser.evaluate(id => localStorage.setItem('yovoice-active-project', id), textSourceID);
  await browser.reload();
  await screen.getByTestId('nav-speakers').tap();
  const originalSource = await browser.evaluate(id => JSON.stringify(window.__yovoiceSyntheticSpeakerFixture.state.drafts.find(item => item.id === id)), textSourceID);
  await enrollBoundProfile(screen);
  await selectGenerationInputs(screen, 'Qwen3-TTS 1.7B Base');
  await browser.locator('#speaker-generation-text').fill('A normal TTS result.');
  const previousCallCount = await generationCallCount(browser);
  await screen.getByTestId('speaker-generate').tap();

  const request = await latestGenerationSnapshot(browser, {
    previousCallCount,
    modelID: 'qwen3-tts-base-q8',
    sourceID: textSourceID,
  });

  expect(request.requestId).toMatch(/^[a-f0-9]{32}$/);
  expect(JSON.stringify(request.source)).toBe(originalSource);
  expect(request.draft.id).toBe(textSourceID);
  expect(request.draft.synthesisLanguage).toBe('en');
  expect(request.draft.modelOptions).toEqual({ index_tts2: { text_chunk_size: 512, text_chunk_mode: 'default' } });
});

test('故事作品生成时不把旧字幕或时间线带入声纹 TTS 请求', async ({ app, browser, screen }) => {
  await serveSyntheticGeneratedOutput(browser);
  await app.open();
  await browser.evaluate(id => {
    localStorage.setItem('yovoice-synthetic-story-source', id);
    localStorage.setItem('yovoice-active-project', id);
  }, textSourceID);
  await browser.reload();
  await screen.getByTestId('nav-speakers').tap();
  const originalSource = await browser.evaluate(id => JSON.stringify(window.__yovoiceSyntheticSpeakerFixture.state.drafts.find(item => item.id === id)), textSourceID);
  const storySource = await browser.evaluate(id => window.__yovoiceSyntheticSpeakerFixture.state.drafts.find(item => item.id === id), textSourceID);
  expect(storySource?.kind).toBe('story');
  expect(storySource?.subtitles?.cues[0]?.text).toBe('old cue');
  expect(storySource?.timeline?.tracks[0]?.id).toBe('story-track');
  expect(storySource?.timeline?.tracks[0]?.clips[0]).toMatchObject({
    id: 'story-clip',
    generationId: 'old-story-generation',
  });
  await enrollBoundProfile(screen);
  await selectGenerationInputs(screen, 'Index TTS 2.5');
  await browser.locator('#speaker-generation-text').fill('Speaker page story target body.');
  const previousCallCount = await generationCallCount(browser);
  await screen.getByTestId('speaker-generate').tap();

  const request = await latestGenerationSnapshot(browser, {
    previousCallCount,
    modelID: 'index-2.5-q8',
    sourceID: textSourceID,
  });
  const sourceAfter = await browser.evaluate(id => JSON.stringify(window.__yovoiceSyntheticSpeakerFixture.state.drafts.find(item => item.id === id)), textSourceID);

  expect(request.requestId).toMatch(/^[a-f0-9]{32}$/);
  expect(request.draft.id).toBe(textSourceID);
  expect(request.draft.text).toBe('Speaker page story target body.');
  expect(Object.hasOwn(request.draft, 'subtitles')).toBe(false);
  expect(Object.hasOwn(request.draft, 'timeline')).toBe(false);
  expect(sourceAfter).toBe(originalSource);
});

test('Index 设置写入其实际使用的语言和情绪参数', async ({ app, browser, screen }) => {
  await serveSyntheticGeneratedOutput(browser);
  await openSpeakerPage(app, screen);
  await enrollBoundProfile(screen);
  await selectGenerationInputs(screen, 'Index TTS 2.5');
  await screen.getByTestId('speaker-index-language').tap();
  await screen.getByRole('option', /日语|Japanese/).tap();
  await screen.getByTestId('speaker-index-expression').getByRole('radio', /情绪调节|Emotion mix/).tap();
  await expect(screen.getByTestId('speaker-index-emotion-vector')).toBeVisible();
  const previousCallCount = await generationCallCount(browser);
  await screen.getByTestId('speaker-generate').tap();

  const request = await latestGenerationSnapshot(browser, {
    previousCallCount,
    modelID: 'index-2.5-q8',
  });

  expect(request.draft.language).toBe('ja');
  expect(request.draft.mode).toBe('vector');
  expect(request.draft.synthesisLanguage).toBe('auto');
});

test('Index 2.5 的日语切换到 Index 2.0 时回退为可提交的中文', async ({ app, browser, screen }) => {
  await serveSyntheticGeneratedOutput(browser);
  await openSpeakerPage(app, screen);
  await enrollBoundProfile(screen);
  await selectGenerationInputs(screen, 'Index TTS 2.5');
  await screen.getByTestId('speaker-index-language').tap();
  await screen.getByRole('option', /日语|Japanese/).tap();

  await screen.getByTestId('speaker-generation-model').tap();
  await screen.getByRole('option', 'IndexTTS 2.0').tap();
  await expect(screen.getByTestId('speaker-index-language')).toContainText(/中文|Chinese/);

  const previousCallCount = await generationCallCount(browser);
  await screen.getByTestId('speaker-generate').tap();
  const request = await latestGenerationSnapshot(browser, {
    previousCallCount,
    modelID: 'index-2-q8',
  });

  expect(request.draft.language).toBe('zh');
});

test('声纹生成保留 OmniVoice 和 VoxCPM 的用户模式，不自动改为克隆', async ({ app, browser, screen }) => {
  await serveSyntheticGeneratedOutput(browser);
  await app.open();
  await browser.evaluate(id => {
    const source = window.__yovoiceSyntheticSpeakerFixture.state.drafts.find(item => item.id === id);
    if (!source) throw new Error('missing synthetic text source');
    source.voiceMode = 'design';
    source.voxMode = 'design';
  }, textSourceID);
  await screen.getByRole('button', 'Synthetic text source').first().tap();
  await screen.getByTestId('nav-speakers').tap();
  await enrollBoundProfile(screen);

  await selectGenerationInputs(screen, 'OmniVoice');
  const omniCallCount = await generationCallCount(browser);
  await screen.getByTestId('speaker-generate').tap();
  const omniRequest = await latestGenerationSnapshot(browser, {
    previousCallCount: omniCallCount,
    modelID: 'omnivoice-q8',
    sourceID: textSourceID,
  });

  await selectGenerationInputs(screen, 'VoxCPM 2');
  const voxCallCount = await generationCallCount(browser);
  await screen.getByTestId('speaker-generate').tap();
  const voxRequest = await latestGenerationSnapshot(browser, {
    previousCallCount: voxCallCount,
    modelID: 'voxcpm2-q8',
    sourceID: textSourceID,
  });

  expect(omniRequest.draft.voiceMode).toBe('design');
  expect(voxRequest.draft.voxMode).toBe('design');
  expect(voxRequest.requestId).not.toBe(omniRequest.requestId);
});

test('空的 Omni 和 Vox 模式明确显示自动绑定行为', async ({ app, browser, screen }) => {
  await serveSyntheticGeneratedOutput(browser);
  await app.open();
  await screen.getByTestId('nav-speakers').tap();
  await enrollBoundProfile(screen);

  await selectGenerationInputs(screen, 'OmniVoice');
  const omniAutomatic = screen.getByTestId('speaker-omni-mode').getByRole('radio', /自动|Auto/);
  await expect(omniAutomatic).toBeVisible();
  await expect(omniAutomatic).toHaveAttribute('aria-checked', 'true');
  const omniCallCount = await generationCallCount(browser);
  await screen.getByTestId('speaker-generate').tap();
  const omniRequest = await latestGenerationSnapshot(browser, {
    previousCallCount: omniCallCount,
    modelID: 'omnivoice-q8',
  });
  expect(omniRequest.draft.voiceMode ?? '').toBe('');

  await selectGenerationInputs(screen, 'VoxCPM 2');
  const voxAutomatic = screen.getByTestId('speaker-vox-mode').getByRole('radio', /自动|Auto/);
  await expect(voxAutomatic).toBeVisible();
  await expect(voxAutomatic).toHaveAttribute('aria-checked', 'true');
  await expect(screen.getByText(/风格指导|Style guidance/)).toBeHidden();
  const voxCallCount = await generationCallCount(browser);
  await screen.getByTestId('speaker-generate').tap();
  const voxRequest = await latestGenerationSnapshot(browser, {
    previousCallCount: voxCallCount,
    modelID: 'voxcpm2-q8',
  });
  expect(voxRequest.draft.voxMode ?? '').toBe('');
  expect(voxRequest.requestId).not.toBe(omniRequest.requestId);
});

test('识别页面按服务的精确 T/M 与原始分数显示 unknown、matched 和 ambiguous', async ({ app, browser, screen }) => {
  await openSpeakerPage(app, screen);
  const stateBefore = await browser.evaluate(() => JSON.stringify(window.__yovoiceSyntheticSpeakerFixture.state));
  const voicesBefore = await browser.evaluate(() => JSON.stringify(window.__yovoiceSyntheticSpeakerFixture.state.voices));
  const generationCallsBefore = await browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.calls.filter(call => call.method === 'speaker.generate').length);

  await configureFixture(browser, {
    identification: {
      status: 'unknown',
      reason: 'service returned no candidates',
      candidates: [],
      threshold: 0.6,
      margin: 0.05,
      modelSha256: 'synthetic-model',
      featureVersion: 'synthetic-version',
    },
  });
  await screen.getByTestId('speaker-identify-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await screen.getByTestId('speaker-identify').tap();
  const result = screen.getByTestId('speaker-result');
  await expect(result).toHaveAttribute('data-status', 'unknown');
  await expect(result).toHaveAttribute('data-profile-id', '');
  await expect(result).toHaveAttribute('data-score', '');
  await expect(result).toHaveAttribute('data-candidate-count', '0');
  await expect(result).toHaveAttribute('data-threshold', '0.6');
  await expect(result).toHaveAttribute('data-margin', '0.05');
  const stateAfterUnknown = await browser.evaluate(() => JSON.stringify(window.__yovoiceSyntheticSpeakerFixture.state));
  const voicesAfterUnknown = await browser.evaluate(() => JSON.stringify(window.__yovoiceSyntheticSpeakerFixture.state.voices));
  expect(stateAfterUnknown).toBe(stateBefore);
  expect(voicesAfterUnknown).toBe(voicesBefore);

  await enrollBoundProfile(screen);
  await screen.getByTestId('speaker-enroll-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await screen.getByTestId('speaker-profile-name').fill('Fixture B');
  await screen.getByTestId('speaker-enroll').tap();
  await expect(screen.getByTestId('speaker-profile-00000000000000000000000000000002')).toBeVisible();

  await configureFixture(browser, {
    identification: {
      status: 'matched',
      reason: 'service matched exactly at the threshold',
      profileId: '00000000000000000000000000000001',
      score: 0.6,
      candidates: [{ profileId: '00000000000000000000000000000001', name: 'Fixture A', score: 0.6 }],
      threshold: 0.6,
      margin: 0.05,
      modelSha256: 'synthetic-model',
      featureVersion: 'synthetic-version',
    },
  });
  await screen.getByTestId('speaker-identify-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await screen.getByTestId('speaker-identify').tap();
  await expect(result).toHaveAttribute('data-status', 'matched');
  await expect(result).toHaveAttribute('data-profile-id', '00000000000000000000000000000001');
  await expect(result).toHaveAttribute('data-score', '0.6');
  await expect(result).toHaveAttribute('data-threshold', '0.6');
  await expect(result).toHaveAttribute('data-margin', '0.05');
  await expect(result).toContainText('Fixture A · 00000000000000000000000000000001 · 0.6');

  await configureFixture(browser, {
    identification: {
      status: 'matched',
      reason: 'service matched at the exact margin boundary',
      profileId: '00000000000000000000000000000001',
      score: 0.8,
      candidates: [
        { profileId: '00000000000000000000000000000001', name: 'Fixture A', score: 0.8 },
        { profileId: '00000000000000000000000000000002', name: 'Fixture B', score: 0.75 },
      ],
      threshold: 0.6,
      margin: 0.05,
      modelSha256: 'synthetic-model',
      featureVersion: 'synthetic-version',
    },
  });
  await screen.getByTestId('speaker-identify-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await screen.getByTestId('speaker-identify').tap();
  await expect(result).toHaveAttribute('data-status', 'matched');
  await expect(result).toHaveAttribute('data-profile-id', '00000000000000000000000000000001');
  await expect(result).toHaveAttribute('data-score', '0.8');
  await expect(result).toHaveAttribute('data-candidate-count', '2');
  await expect(screen.getByTestId('speaker-candidate-00000000000000000000000000000001')).toHaveText('Fixture A · 00000000000000000000000000000001 · 0.8');
  await expect(screen.getByTestId('speaker-candidate-00000000000000000000000000000002')).toHaveText('Fixture B · 00000000000000000000000000000002 · 0.75');

  await configureFixture(browser, {
    identification: {
      status: 'ambiguous',
      reason: 'service kept same-rounded raw scores ambiguous below the margin',
      candidates: [
        { profileId: '00000000000000000000000000000001', name: 'Duplicate candidate', score: 0.80441 },
        { profileId: '00000000000000000000000000000002', name: 'Duplicate candidate', score: 0.8044 },
      ],
      threshold: 0.8,
      margin: 0.0001,
      modelSha256: 'synthetic-model',
      featureVersion: 'synthetic-version',
    },
  });
  await screen.getByTestId('speaker-identify-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await screen.getByTestId('speaker-identify').tap();
  await expect(result).toHaveAttribute('data-status', 'ambiguous');
  await expect(result).toHaveAttribute('data-profile-id', '');
  await expect(result).toHaveAttribute('data-threshold', '0.8');
  await expect(result).toHaveAttribute('data-margin', '0.0001');
  await expect(screen.getByTestId('speaker-candidate-00000000000000000000000000000001')).toHaveText('Duplicate candidate · 00000000000000000000000000000001 · 0.80441');
  await expect(screen.getByTestId('speaker-candidate-00000000000000000000000000000002')).toHaveText('Duplicate candidate · 00000000000000000000000000000002 · 0.8044');

  await expect(screen.getByRole('combobox', /选择已绑定档案|Choose a bound profile/)).toHaveValue('');
  const generationCallsAfter = await browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.calls.filter(call => call.method === 'speaker.generate').length);
  expect(generationCallsAfter).toBe(generationCallsBefore);
});

test('运行时状态显示服务保留的零阈值和亚秒超时', async ({ app, browser, screen }) => {
  await openSpeakerPage(app, screen);
  await configureFixture(browser, {
    runtime: {
      configured: true,
      threshold: 0,
      margin: 0.05,
      timeoutSeconds: 0.5,
      modelSha256: 'synthetic-model',
      featureVersion: 'synthetic-version',
      dimension: 256,
    },
  });

  await screen.getByRole('button', /刷新|Refresh/).first().tap();
  await expect(screen.getByTestId('speaker-runtime')).toContainText(/0.*0\.05.*0\.5/);
});

test('缺少识别配置时保留输入并在恢复配置后允许登记', async ({ app, browser, screen }) => {
  await openSpeakerPage(app, screen);
  await enrollBoundProfile(screen);
  const existingProfile = screen.getByTestId('speaker-profile-00000000000000000000000000000001');
  await configureFixture(browser, { runtimeConfigured: false });
  await screen.getByRole('button', /刷新|Refresh/).first().tap();
  await screen.getByTestId('speaker-enroll-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await expect(screen.getByTestId('speaker-enroll')).toBeDisabled();
  await expect(screen.getByRole('alert')).toBeVisible();
  await expect(existingProfile).toBeVisible();
  await screen.getByTestId('speaker-delete-00000000000000000000000000000001').tap();
  await screen.getByRole('button', /^(删除档案|Delete profile)$/).tap();
  await expect(existingProfile).toBeHidden();

  await configureFixture(browser, { runtimeConfigured: true });
  await screen.getByRole('button', /刷新|Refresh/).first().tap();
  await expect(screen.getByTestId('speaker-enroll')).toBeEnabled();
});

test('麦克风权限或设备缺失后可使用文件输入恢复', async ({ app, browser, screen }) => {
  await openSpeakerPage(app, screen);
  for (const errorName of ['NotAllowedError', 'NotFoundError']) {
    await browser.evaluate(name => {
      Object.defineProperty(navigator, 'mediaDevices', {
        configurable: true,
        value: {
          getUserMedia: () => Promise.reject(Object.assign(new Error(name), { name })),
        },
      });
      return typeof navigator.mediaDevices?.getUserMedia === 'function';
    }, errorName).then(applied => expect(applied).toBe(true));
    await screen.getByTestId('speaker-record-enroll').tap();
    await expect(screen.getByRole('alert')).toBeVisible();
    await screen.getByTestId('speaker-enroll-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
    await expect(screen.getByTestId('speaker-enroll')).toBeEnabled();
  }
});

test('录音到达上限后显示可恢复提示，换输入会清除旧提示', async ({ app, browser, screen }) => {
  await openSpeakerPage(app, screen);
  await mockRecording(browser);
  await browser.evaluate(() => {
    const target = globalThis as typeof globalThis & {
      speakerAutoStop?: () => void;
      speakerOriginalSetTimeout?: typeof setTimeout;
    };
    const originalSetTimeout = globalThis.setTimeout;
    target.speakerOriginalSetTimeout = originalSetTimeout;
    globalThis.setTimeout = ((callback: TimerHandler, timeout?: number, ...args: unknown[]) => {
      if (timeout === 60_000 && typeof callback === 'function') {
        target.speakerAutoStop = () => callback(...args);
        return 0 as unknown as ReturnType<typeof setTimeout>;
      }
      return originalSetTimeout(callback, timeout, ...args);
    }) as typeof setTimeout;
  });

  await screen.getByTestId('speaker-record-enroll').tap();
  await expect(screen.getByText(/正在录音|Recording 0s/)).toBeVisible();
  await waitFor(browser, 1_100);
  const autoStopInstalled = await browser.evaluate(() => {
    const target = globalThis as typeof globalThis & { speakerAutoStop?: () => void };
    target.speakerAutoStop?.();
    return Boolean(target.speakerAutoStop);
  });
  expect(autoStopInstalled).toBe(true);
  await expect(screen.getByText(/录音已在 60 秒后自动停止|Recording stopped automatically after 60 seconds/)).toBeVisible();
  await expect(screen.getByText(/临时录音|Recorded temporary audio/)).toBeVisible();
  const voicesBeforeEnrollment = await browser.evaluate(() => JSON.stringify(window.__yovoiceSyntheticSpeakerFixture.state.voices));
  await screen.getByTestId('speaker-profile-name').fill('Unbound recording');
  await screen.getByTestId('speaker-enroll').tap();
  await expect(screen.getByTestId('speaker-profile-00000000000000000000000000000001')).toBeVisible();
  const recordedProfile = await browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.profiles[0]);
  const voicesAfterEnrollment = await browser.evaluate(() => JSON.stringify(window.__yovoiceSyntheticSpeakerFixture.state.voices));
  expect(recordedProfile.bindVoiceId).toBeUndefined();
  expect(voicesAfterEnrollment).toBe(voicesBeforeEnrollment);

  await screen.getByTestId('speaker-enroll-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await expect(screen.getByText(/录音已在 60 秒后自动停止|Recording stopped automatically after 60 seconds/)).toBeHidden();
  await browser.evaluate(() => {
    const target = globalThis as typeof globalThis & { speakerOriginalSetTimeout?: typeof setTimeout };
    if (target.speakerOriginalSetTimeout) globalThis.setTimeout = target.speakerOriginalSetTimeout;
    delete target.speakerAutoStop;
    delete target.speakerOriginalSetTimeout;
  });
});

test('接受的取消不会显示迟到的识别成功', async ({ app, browser, screen }) => {
  await openSpeakerPage(app, screen);
  await enrollBoundProfile(screen);
  await screen.getByTestId('speaker-identify-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await screen.getByTestId('speaker-identify').tap();
  await expect(screen.getByTestId('speaker-result')).toHaveAttribute('data-status', 'matched');

  await configureFixture(browser, { cancelResults: [false, false, true], identifyDelayMs: 2_000 });
  await screen.getByTestId('speaker-identify-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await screen.getByTestId('speaker-identify').tap();
  const operationRequestId = await browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.calls
    .filter(call => call.method === 'speaker.identify')
    .at(-1)?.data.requestId);
  expect(operationRequestId).toMatch(/^[a-f0-9]{32}$/);
  const cancelCallsBefore = await browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.calls.filter(call => call.method === 'speaker.cancel').length);
  await cancelButton(screen).tap();
  await expect(screen.getByText(/已请求取消|Cancellation was requested/)).toBeVisible();
  const cancellationCalls = await browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.calls.filter(call => call.method === 'speaker.cancel').length);
  expect(cancellationCalls - cancelCallsBefore).toBe(3);
  const cancellationRequestIDs = await browser.evaluate(start => window.__yovoiceSyntheticSpeakerFixture.calls
    .filter(call => call.method === 'speaker.cancel')
    .slice(start)
    .map(call => call.data.requestId), cancelCallsBefore);
  expect(cancellationRequestIDs).toEqual([operationRequestId, operationRequestId, operationRequestId]);
  await waitFor(browser, 2_100);
  await expect(screen.getByTestId('speaker-result')).toBeHidden();
});

test('无取消回复时在五秒窗口内有界重试，并允许用户再次尝试', async ({ app, browser, screen }) => {
  await openSpeakerPage(app, screen);
  await configureFixture(browser, { cancelResults: ['none'], identifyDelayMs: 20_000 });
  await screen.getByTestId('speaker-identify-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await screen.getByTestId('speaker-identify').tap();
  const operationRequestId = await browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.calls
    .filter(call => call.method === 'speaker.identify')
    .at(-1)?.data.requestId);
  expect(operationRequestId).toMatch(/^[a-f0-9]{32}$/);
  const cancelCallsBefore = await browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.calls.filter(call => call.method === 'speaker.cancel').length);
  await cancelButton(screen).tap();
  await expect(screen.getByText(/无法确认是否取消|Cancellation could not be confirmed/)).toBeVisible({ timeout: 6_000 });
  const cancellationCalls = await browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.calls.filter(call => call.method === 'speaker.cancel').length);
  expect(cancellationCalls - cancelCallsBefore).toBeGreaterThanOrEqual(3);
  expect(cancellationCalls - cancelCallsBefore).toBeLessThanOrEqual(11);
  const cancellationRequestIDs = await browser.evaluate(start => window.__yovoiceSyntheticSpeakerFixture.calls
    .filter(call => call.method === 'speaker.cancel')
    .slice(start)
    .map(call => call.data.requestId), cancelCallsBefore);
  expect(cancellationRequestIDs).toEqual(Array(cancellationRequestIDs.length).fill(operationRequestId));
  await waitFor(browser, 600);
  const cancellationCallsAfterWindow = await browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.calls.filter(call => call.method === 'speaker.cancel').length);
  expect(cancellationCallsAfterWindow).toBe(cancellationCalls);
  await configureFixture(browser, { cancelResults: [true] });
  await cancelButton(screen).tap();
  await expect(screen.getByText(/已请求取消|Cancellation was requested/)).toBeVisible();
  await expect(screen.getByTestId('speaker-result')).toBeHidden();
});

test('取消登记后服务已接受时刷新并保留真实成功', async ({ app, browser, screen }) => {
  await openSpeakerPage(app, screen);
  await configureFixture(browser, { cancelResults: [true], holdEnrollResponses: true });
  await screen.getByTestId('speaker-enroll-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await screen.getByTestId('speaker-profile-name').fill('Accepted registration');
  const heldEnrollment = waitForHeldEnrollResponse(browser);
  await screen.getByTestId('speaker-enroll').tap();
  const enrollment = await heldEnrollment;
  expect(enrollment.profileID).toBe('00000000000000000000000000000001');
  expect(enrollment.requestID).toMatch(/^[a-f0-9]{32}$/);
  await expect(cancelButton(screen)).toBeVisible();
  await cancelButton(screen).tap();
  const cancellationRequestIDs = await browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.calls
    .filter(call => call.method === 'speaker.cancel')
    .map(call => call.data.requestId));
  expect(cancellationRequestIDs).toEqual([enrollment.requestID]);
  await screen.getByTestId('speaker-enroll-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await releaseHeldEnrollResponse(browser, enrollment.id);
  await expect(screen.getByTestId('speaker-profile-00000000000000000000000000000001')).toBeVisible();
  await expect(screen.getByText(/已登记 Accepted registration。|Registered Accepted registration./)).toBeVisible();
});

test('登记响应未知时不会自动重试，刷新确认后显式再登记会得到独立 ID', async ({ app, browser, screen }) => {
  await openSpeakerPage(app, screen);
  await configureFixture(browser, {
    cancelResults: [true],
    enrollPostAcceptErrorCode: '@yovoice.error.speakerTimeout',
  });
  await screen.getByTestId('speaker-enroll-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await screen.getByTestId('speaker-profile-name').fill('Unknown response');
  await screen.getByTestId('speaker-enroll').tap();

  await expect(screen.getByTestId('speaker-profile-00000000000000000000000000000001')).toBeVisible();
  const enrollCallsAfterUnknownResponse = await browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.calls
    .filter(call => call.method === 'speaker.enroll').length);
  expect(enrollCallsAfterUnknownResponse).toBe(1);

  await screen.getByRole('button', /刷新|Refresh/).first().tap();
  await expect(screen.getByTestId('speaker-profile-00000000000000000000000000000001')).toBeVisible();
  await configureFixture(browser, { enrollPostAcceptErrorCode: null });
  await screen.getByTestId('speaker-enroll').tap();
  await expect(screen.getByTestId('speaker-profile-00000000000000000000000000000002')).toBeVisible();
  const enrollCallsAfterExplicitRetry = await browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.calls
    .filter(call => call.method === 'speaker.enroll').length);
  expect(enrollCallsAfterExplicitRetry).toBe(2);
});

test('替换识别输入会取消旧操作，后续操作仍可完成', async ({ app, browser, screen }) => {
  await openSpeakerPage(app, screen);
  await enrollBoundProfile(screen);
  await configureFixture(browser, { cancelResults: [true], identifyDelayMs: 300 });
  await screen.getByTestId('speaker-identify-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await screen.getByTestId('speaker-identify').tap();
  await screen.getByTestId('speaker-identify-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await waitFor(browser, 500);
  await expect(screen.getByTestId('speaker-result')).toBeHidden();

  await screen.getByTestId('speaker-identify').tap();
  await expect(screen.getByTestId('speaker-result')).toHaveAttribute('data-status', 'matched');
});

test('重复标签保留独立档案 ID，确认删除只移除指定档案', async ({ app, browser, screen }) => {
  await openSpeakerPage(app, screen);
  await enrollBoundProfile(screen);
  await screen.getByTestId('speaker-enroll-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await screen.getByTestId('speaker-profile-name').fill('Fixture A');
  await screen.getByTestId('speaker-enroll').tap();
  await expect(screen.getByTestId('speaker-profile-00000000000000000000000000000002')).toBeVisible();
  const longIDLayout = await browser.evaluate(() => {
    const row = document.querySelector<HTMLElement>('[data-speaker-profile-row="true"]');
    const identifier = Array.from(row?.querySelectorAll('small') ?? [])
      .find(element => /^[a-f0-9]{32}$/.test(element.textContent ?? '')) as HTMLElement | undefined;
    if (!row || !identifier) return null;
    const rowRect = row.getBoundingClientRect();
    return {
      identifierFits: identifier.scrollWidth <= identifier.clientWidth,
      rowFitsViewport: rowRect.left >= 0 && rowRect.right <= window.innerWidth,
    };
  });
  expect(longIDLayout).toEqual({ identifierFits: true, rowFitsViewport: true });
  await screen.getByTestId('speaker-delete-00000000000000000000000000000001').tap();
  await screen.getByRole('button', /取消|Cancel/).tap();
  await expect(screen.getByTestId('speaker-profile-00000000000000000000000000000001')).toBeVisible();
  await screen.getByTestId('speaker-delete-00000000000000000000000000000001').tap();
  await screen.getByRole('button', /^(删除档案|Delete profile)$/).tap();
  await expect(screen.getByTestId('speaker-profile-00000000000000000000000000000001')).toBeHidden();
  await expect(screen.getByTestId('speaker-profile-00000000000000000000000000000002')).toBeVisible();
});

test('列表错误不会伪装为空库，刷新可加载外部新增档案', async ({ app, browser, screen }) => {
  await openSpeakerPage(app, screen);
  await enrollBoundProfile(screen);
  const existingProfile = screen.getByTestId('speaker-profile-00000000000000000000000000000001');
  await expect(existingProfile).toBeVisible();

  await configureFixture(browser, { listErrorCode: '@yovoice.error.runtimeFailed' });
  await screen.getByRole('button', /刷新|Refresh/).first().tap();
  await expect(screen.getByTestId('speaker-profiles')).toHaveAttribute('data-state', 'error');
  await expect(existingProfile).toBeVisible();

  await browser.evaluate(() => {
    window.__yovoiceSyntheticSpeakerFixture.profiles.push({
      id: '00000000000000000000000000000002',
      name: 'External profile',
      createdAt: '2026-10-12T00:00:00Z',
      modelSha256: 'synthetic-model',
      featureVersion: 'synthetic-version',
      dimension: 256,
    });
  });
  await configureFixture(browser, { listErrorCode: null });
  await screen.getByRole('button', /刷新|Refresh/).first().tap();
  await expect(screen.getByTestId('speaker-profiles')).toHaveAttribute('data-state', 'ready');
  await expect(screen.getByTestId('speaker-profile-00000000000000000000000000000002')).toBeVisible();
});

test('迟到的档案读取或错误不会覆盖最新删除后的刷新结果', async ({ app, browser, screen }) => {
  await openSpeakerPage(app, screen);
  await enrollBoundProfile(screen);
  const profile = screen.getByTestId('speaker-profile-00000000000000000000000000000001');
  const refresh = screen.getByRole('button', /刷新|Refresh/).first();

  await configureFixture(browser, { holdListResponses: true, listErrorCode: null });
  const oldSnapshot = waitForHeldListResponse(browser);
  await refresh.tap();
  const oldSnapshotRequest = await oldSnapshot;
  expect(oldSnapshotRequest.profileIDs).toEqual(['00000000000000000000000000000001']);

  await configureFixture(browser, { holdListResponses: false });
  await screen.getByTestId('speaker-delete-00000000000000000000000000000001').tap();
  await screen.getByRole('button', /^(删除档案|Delete profile)$/).tap();
  await expect(screen.getByTestId('speaker-profiles')).toHaveAttribute('data-state', 'ready');
  await expect(profile).toHaveCount(0);

  await releaseHeldListResponse(browser, oldSnapshotRequest.id);
  await expect(screen.getByTestId('speaker-profiles')).toHaveAttribute('data-state', 'ready');
  await expect(profile).toHaveCount(0);

  await configureFixture(browser, {
    holdListResponses: true,
    listErrorCode: '@yovoice.error.runtimeFailed',
  });
  const oldError = waitForHeldListResponse(browser);
  await refresh.tap();
  const oldErrorRequest = await oldError;
  expect(oldErrorRequest.errorCode).toBe('@yovoice.error.runtimeFailed');

  await configureFixture(browser, { holdListResponses: false, listErrorCode: null });
  await refresh.tap();
  await expect(screen.getByTestId('speaker-profiles')).toHaveAttribute('data-state', 'ready');

  await releaseHeldListResponse(browser, oldErrorRequest.id);
  await expect(screen.getByTestId('speaker-profiles')).toHaveAttribute('data-state', 'ready');
  await expect(screen.getByRole('alert')).toHaveCount(0);
});

test('删除成功后即使列表或运行时刷新失败也立即移除精确档案', async ({ app, browser, screen }) => {
  await openSpeakerPage(app, screen);
  await enrollBoundProfile(screen);

  const firstProfileID = '00000000000000000000000000000001';
  const secondProfileID = '00000000000000000000000000000002';
  const thirdProfileID = '00000000000000000000000000000003';
  const generationProfile = screen.getByRole('combobox', /选择已绑定档案|Choose a bound profile/);

  for (const name of ['Fixture B', 'Fixture C']) {
    await screen.getByTestId('speaker-enroll-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
    await screen.getByTestId('speaker-profile-name').fill(name);
    await screen.getByTestId('speaker-profile-bind-voice').tap();
    await screen.getByRole('option', 'Bound voice').tap();
    await screen.getByTestId('speaker-enroll').tap();
  }
  await expect(screen.getByTestId(`speaker-profile-${thirdProfileID}`)).toBeVisible();

  await screen.getByTestId('speaker-generation-profile').tap();
  await screen.getByRole('option', /Fixture A/).tap();
  await expect(generationProfile).toContainText(/Fixture A/);

  await configureFixture(browser, { listErrorCode: '@yovoice.error.runtimeFailed' });
  await screen.getByTestId(`speaker-delete-${firstProfileID}`).tap();
  await screen.getByTestId('speaker-delete-confirm').tap();
  await expect(screen.getByTestId('speaker-profiles')).toHaveAttribute('data-state', 'error');
  await expect(screen.getByTestId('speaker-profiles').getByRole('alert')).toBeVisible();
  await expect(screen.getByTestId(`speaker-profile-${firstProfileID}`)).toHaveCount(0);
  await expect(screen.getByTestId(`speaker-profile-${secondProfileID}`)).toBeVisible();
  await expect(screen.getByTestId(`speaker-profile-${thirdProfileID}`)).toBeVisible();
  await expect(generationProfile).toContainText(/选择已绑定档案|Choose a bound profile/);

  await screen.getByTestId('speaker-generation-profile').tap();
  await expect(screen.getByRole('option', /Fixture A/)).toHaveCount(0);
  await screen.getByRole('option', /Fixture B/).tap();
  await expect(generationProfile).toContainText(/Fixture B/);

  await configureFixture(browser, { listErrorCode: null });
  await screen.getByRole('button', /刷新|Refresh/).first().tap();
  await expect(screen.getByTestId('speaker-profiles')).toHaveAttribute('data-state', 'ready');

  await configureFixture(browser, { runtimeErrorCode: '@yovoice.error.runtimeFailed' });
  await screen.getByTestId(`speaker-delete-${thirdProfileID}`).tap();
  await screen.getByTestId('speaker-delete-confirm').tap();
  await expect(screen.getByTestId('speaker-profiles')).toHaveAttribute('data-state', 'error');
  await expect(screen.getByTestId('speaker-profiles').getByRole('alert')).toBeVisible();
  await expect(screen.getByTestId(`speaker-profile-${thirdProfileID}`)).toHaveCount(0);
  await expect(screen.getByTestId(`speaker-profile-${secondProfileID}`)).toBeVisible();
  await expect(generationProfile).toContainText(/Fixture B/);

  const deletionIDs = await browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.calls
    .filter(call => call.method === 'speaker.delete')
    .map(call => call.data.id));
  expect(deletionIDs).toEqual([firstProfileID, thirdProfileID]);

  await configureFixture(browser, { runtimeErrorCode: null });
  await screen.getByRole('button', /刷新|Refresh/).first().tap();
  await expect(screen.getByTestId('speaker-profiles')).toHaveAttribute('data-state', 'ready');
  await expect(screen.getByTestId(`speaker-profile-${firstProfileID}`)).toHaveCount(0);
  await expect(screen.getByTestId(`speaker-profile-${secondProfileID}`)).toBeVisible();
  await expect(screen.getByTestId(`speaker-profile-${thirdProfileID}`)).toHaveCount(0);
  await expect(screen.getByRole('alert')).toHaveCount(0);
});

test('没有可用 TTS 时登记仍可用，生成提示保留正文且不发起假生成', async ({ app, browser, screen }) => {
  await app.open();
  await browser.evaluate(() => localStorage.setItem('yovoice-synthetic-no-tts-models', 'true'));
  await browser.reload();
  await openSpeakerPage(app, screen);
  await enrollBoundProfile(screen);
  await browser.locator('#speaker-generation-text').fill('Keep this text until a TTS model is available.');

  await expect(screen.getByTestId('speaker-generate')).toBeDisabled();
  await expect(screen.getByText(/生成需要|Generation needs/)).toBeVisible();
  await expect(browser.locator('#speaker-generation-text')).toHaveValue('Keep this text until a TTS model is available.');
  const generationCalls = await generationCallCount(browser);
  expect(generationCalls).toBe(0);

  await browser.evaluate(() => localStorage.removeItem('yovoice-synthetic-no-tts-models'));
});

test('档案行标记在异步列表结束后只计算真实档案，并在删除后保留独立 ID', async ({ app, browser, screen }) => {
  await app.open();
  await configureFixture(browser, { listDelayMs: 150 });
  await screen.getByTestId('nav-speakers').tap();
  await expect(screen.getByTestId('speaker-profiles')).toHaveAttribute('data-state', 'ready');

  await enrollBoundProfile(screen);
  await screen.getByTestId('speaker-enroll-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await screen.getByTestId('speaker-profile-name').fill('Fixture B');
  await screen.getByTestId('speaker-enroll').tap();
  const rows = browser.locator('[data-speaker-profile-row="true"]');
  await expect(rows).toHaveCount(2);

  await screen.getByTestId('speaker-delete-00000000000000000000000000000001').tap();
  await screen.getByRole('button', /^(删除档案|Delete profile)$/).tap();
  await expect(rows).toHaveCount(1);
  await expect(screen.getByTestId('speaker-profile-00000000000000000000000000000002')).toHaveAttribute('data-profile-id', '00000000000000000000000000000002');
});

test('取消生成后若服务端实际成功，页面保留真实成功结果', async ({ app, browser, screen }) => {
  await serveSyntheticGeneratedOutput(browser);
  await openSpeakerPage(app, screen);
  await enrollBoundProfile(screen);
  await selectGenerationInputs(screen, 'Qwen3-TTS 1.7B Base');
  await browser.locator('#speaker-generation-text').fill('Keep a committed generation result.');
  await configureFixture(browser, { cancelResults: [true], generateDelayMs: 300 });
  await screen.getByTestId('speaker-generate').tap();
  await cancelButton(screen).tap();
  await waitFor(browser, 500);
  await expect(screen.getByText(/已使用所选绑定档案开始生成。|Generation started with the selected bound profile./)).toBeVisible();
  await expect(screen.getByTestId('speaker-generated-output')).toBeVisible();
});

test('关闭声纹页时仅取消当前操作，迟到结果不写回已关闭页面', async ({ app, browser, screen }) => {
  await openSpeakerPage(app, screen);
  await configureFixture(browser, { cancelResults: [true], identifyDelayMs: 300 });
  await screen.getByTestId('speaker-identify-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await screen.getByTestId('speaker-identify').tap();
  await screen.getByTestId('nav-characters').tap();
  await waitFor(browser, 500);
  const calls = await browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.calls.filter(call => call.method === 'speaker.cancel').length);
  expect(calls).toBe(1);
  await expect(screen.getByTestId('speaker-result')).toBeHidden();
});

test('键盘可激活登记、刷新、删除与显式生成，且焦点样式可见', async ({ app, browser, screen }) => {
  await openSpeakerPage(app, screen);
  await screen.getByTestId('speaker-enroll-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  const name = screen.getByTestId('speaker-profile-name');
  const binding = screen.getByTestId('speaker-profile-bind-voice').getByRole('combobox');
  const enroll = screen.getByTestId('speaker-enroll');
  await name.focus();
  await name.pressSequentially('Keyboard profile');
  await expect(name).toHaveValue('Keyboard profile');
  await name.press('Tab');
  await expect(binding).toBeFocused();
  await binding.press('Tab');
  await expect(enroll).toBeFocused();
  await enroll.press('Shift+Tab');
  await expect(binding).toBeFocused();
  await binding.press('Tab');
  await expect(enroll).toBeFocused();
  await enroll.focus();
  await expect(enroll).toBeFocused();
  const focusRing = await browser.evaluate(() => {
    const element = document.querySelector<HTMLElement>('[data-testid="speaker-enroll"]');
    if (!element) return null;
    const style = getComputedStyle(element);
    return {
      focusVisible: element.matches(':focus-visible'),
      outlineVisible: style.outlineStyle !== 'none' && style.outlineWidth !== '0px',
      boxShadowVisible: style.boxShadow !== 'none',
    };
  });
  expect(focusRing?.focusVisible).toBe(true);
  expect(focusRing?.outlineVisible || focusRing?.boxShadowVisible).toBe(true);
  await enroll.press('Enter');
  await expect(screen.getByTestId('speaker-profile-00000000000000000000000000000001')).toBeVisible();

  const refresh = screen.getByRole('button', /刷新|Refresh/).first();
  await refresh.focus();
  await expect(refresh).toBeFocused();
  await refresh.press('Enter');
  await expect(screen.getByTestId('speaker-profiles')).toHaveAttribute('data-state', 'ready');

  const remove = screen.getByTestId('speaker-delete-00000000000000000000000000000001');
  await remove.focus();
  await remove.press('Enter');
  const cancel = screen.getByRole('button', /取消|Cancel/);
  await cancel.focus();
  await expect(cancel).toBeFocused();
  await cancel.press('Enter');
  await expect(screen.getByTestId('speaker-profile-00000000000000000000000000000001')).toBeVisible();

  await remove.focus();
  await remove.press('Enter');
  const confirm = screen.getByTestId('speaker-delete-confirm');
  await confirm.focus();
  await expect(confirm).toBeFocused();
  await confirm.press('Enter');
  await expect(screen.getByTestId('speaker-profile-00000000000000000000000000000001')).toBeHidden();

  await screen.getByTestId('speaker-enroll-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await screen.getByTestId('speaker-profile-name').fill('Fixture A');
  await screen.getByTestId('speaker-profile-bind-voice').tap();
  await screen.getByRole('option', 'Bound voice').tap();
  await screen.getByTestId('speaker-enroll').tap();
  await expect(screen.getByTestId('speaker-profile-00000000000000000000000000000002')).toBeVisible();
  await selectGenerationInputs(screen, 'Qwen3-TTS 1.7B Base');
  await browser.locator('#speaker-generation-text').fill('Keyboard generation action.');
  const generate = screen.getByTestId('speaker-generate');
  await generate.focus();
  await expect(generate).toBeFocused();
  await generate.press('Enter');
  await expect(screen.getByTestId('speaker-generated-output')).toBeVisible();

  const colorSchemes = await browser.evaluate(() => {
    const root = document.documentElement;
    const previous = root.getAttribute('data-theme');
    root.setAttribute('data-theme', 'dark');
    const dark = getComputedStyle(root).colorScheme;
    if (previous === null) root.removeAttribute('data-theme');
    else root.setAttribute('data-theme', previous);
    return { dark, system: getComputedStyle(root).colorScheme };
  });
  expect(colorSchemes.dark).toContain('dark');
  expect(colorSchemes.system).toContain('light');
});

test('生成成功时保留独立输出的试听与打开位置入口', async ({ app, browser, screen }) => {
  await serveSyntheticGeneratedOutput(browser);
  await openSpeakerPage(app, screen);
  await enrollBoundProfile(screen);
  await selectGenerationInputs(screen, 'Qwen3-TTS 1.7B Base');
  await browser.locator('#speaker-generation-text').fill('Expose this output without changing the source project.');
  await screen.getByTestId('speaker-generate').tap();

  const boundReference = screen.getByTestId('speaker-bound-reference');
  await expect(boundReference.getByRole('button', /播放|Play/)).toBeEnabled();
  await boundReference.getByRole('button', /播放|Play/).tap();
  await expect(boundReference.getByRole('button', /暂停|Pause/)).toBeVisible();

  const output = screen.getByTestId('speaker-generated-output');
  await expect(output).toHaveAttribute('data-output-id', 'generation-synthetic');
  await expect(output.getByRole('button', /播放|Play/)).toBeEnabled();
  await output.getByRole('button', /播放|Play/).tap();
  await expect(output.getByRole('button', /暂停|Pause/)).toBeVisible();
  await output.getByRole('button', /打开音频位置|Open audio location/).tap();

  const reveal = await browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.calls.find(call => call.method === 'media.reveal'));
  expect(reveal?.data).toEqual({ kind: 'outputs', id: 'generation-synthetic' });
});

test('新的生成失败时不把上一轮输出显示成当前成功', async ({ app, browser, screen }) => {
  await serveSyntheticGeneratedOutput(browser);
  await openSpeakerPage(app, screen);
  await enrollBoundProfile(screen);
  await selectGenerationInputs(screen, 'Qwen3-TTS 1.7B Base');
  await screen.getByTestId('speaker-generate').tap();
  await expect(screen.getByTestId('speaker-generated-output')).toBeVisible();

  await configureFixture(browser, { generateErrorCode: '@yovoice.error.runtimeFailed' });
  await screen.getByTestId('speaker-generate').tap();
  await expect(screen.getByTestId('speaker-generated-output')).toBeHidden();
});

test('合成录音 mock 开始时会暂停并禁用独立输出播放', async ({ app, browser, screen }) => {
  await serveSyntheticGeneratedOutput(browser);
  await openSpeakerPage(app, screen);
  await enrollBoundProfile(screen);
  await selectGenerationInputs(screen, 'Qwen3-TTS 1.7B Base');
  await browser.locator('#speaker-generation-text').fill('Pause output while recording.');
  await screen.getByTestId('speaker-generate').tap();

  const output = screen.getByTestId('speaker-generated-output');
  await output.getByRole('button', /播放|Play/).tap();
  await expect(output.getByRole('button', /暂停|Pause/)).toBeVisible();
  const playingBeforeRecording = await browser.evaluate(() => {
    const audio = document.querySelector<HTMLAudioElement>('[data-testid="speaker-generated-output"] audio');
    return Boolean(audio && !audio.paused && audio.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA);
  });
  expect(playingBeforeRecording).toBe(true);

  await mockRecording(browser);
  await screen.getByTestId('speaker-record-enroll').tap();
  await expect(output.getByRole('button', /播放|Play/)).toBeDisabled();
  const pausedForRecording = await browser.evaluate(() => document.querySelector<HTMLAudioElement>('[data-testid="speaker-generated-output"] audio')?.paused);
  expect(pausedForRecording).toBe(true);
});

test('任一录音开始时会暂停并拦截另一输入区的预览播放', async ({ app, browser, screen }) => {
  await openSpeakerPage(app, screen);
  await screen.getByTestId('speaker-identify-file').setInputFiles('e2e/fixtures/generated-output.wav');
  const preview = browser.locator('[data-testid="speaker-input-preview-identify"]');
  await expect(preview).toHaveAttribute('aria-disabled', 'false');

  await mockRecording(browser);
  await screen.getByTestId('speaker-record-enroll').tap();
  await expect(preview).toHaveAttribute('aria-disabled', 'true');
  await expect(preview).toHaveAttribute('tabindex', '-1');

  const remainedPaused = await browser.evaluate(async () => {
    const audio = document.querySelector<HTMLAudioElement>('[data-testid="speaker-input-preview-identify"]');
    if (!audio) throw new Error('missing identify preview');
    await audio.play().catch(() => undefined);
    await new Promise(resolve => setTimeout(resolve, 0));
    return audio.paused;
  });
  expect(remainedPaused).toBe(true);
});

test('更换、清除和离开声纹页都会释放实际页面的临时试听 URL', async ({ app, browser, screen }) => {
  await openSpeakerPage(app, screen);
  await browser.evaluate(() => {
    const target = globalThis as typeof globalThis & {
      speakerTemporaryAudioProbe?: TemporaryAudioProbe;
    };
    const probe = {
      created: [] as string[],
      revoked: [] as string[],
      originalCreate: URL.createObjectURL.bind(URL),
      originalRevoke: URL.revokeObjectURL.bind(URL),
    };
    target.speakerTemporaryAudioProbe = probe;
    URL.createObjectURL = value => {
      const url = probe.originalCreate(value);
      probe.created.push(url);
      return url;
    };
    URL.revokeObjectURL = value => {
      probe.revoked.push(value);
      probe.originalRevoke(value);
    };
  });

  const voicesBefore = await browser.evaluate(() => JSON.stringify(window.__yovoiceSyntheticSpeakerFixture.state.voices));
  await screen.getByTestId('speaker-enroll-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await expect(screen.getByTestId('speaker-input-preview-enroll')).toBeVisible();
  const first = await waitForTemporaryAudioProbe(browser, { created: 1, revoked: 0 });
  expect(first.created).toHaveLength(1);

  await screen.getByTestId('speaker-enroll-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  const replaced = await waitForTemporaryAudioProbe(browser, { created: 2, revoked: 1 });
  expect(replaced.created).toHaveLength(2);
  expect(replaced.revoked).toEqual([first.created[0]]);

  await screen.getByRole('button', /清除输入|Clear input/).tap();
  await expect(screen.getByTestId('speaker-input-preview-enroll')).toBeHidden();
  const cleared = await waitForTemporaryAudioProbe(browser, { created: 2, revoked: 2 });
  expect(cleared.revoked).toEqual(cleared.created);

  await screen.getByTestId('speaker-enroll-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await expect(screen.getByTestId('speaker-input-preview-enroll')).toBeVisible();
  await screen.getByTestId('nav-text').tap();
  await waitForTemporaryAudioProbe(browser, { created: 3, revoked: 3 });
  const released = await browser.evaluate(() => {
    const probe = window.speakerTemporaryAudioProbe;
    if (!probe) return null;
    URL.createObjectURL = probe.originalCreate;
    URL.revokeObjectURL = probe.originalRevoke;
    return { created: [...probe.created], revoked: [...probe.revoked] };
  });
  expect(released?.created).toHaveLength(3);
  expect(released?.revoked).toEqual(released?.created);
  const voicesAfter = await browser.evaluate(() => JSON.stringify(window.__yovoiceSyntheticSpeakerFixture.state.voices));
  expect(voicesAfter).toBe(voicesBefore);
});

test('登记输入拒绝及识别错误或繁忙后保留可修正输入且不自动重发', async ({ app, browser, screen }) => {
  await openSpeakerPage(app, screen);
  const enrollmentCallsBefore = await browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.calls
    .filter(call => call.method === 'speaker.enroll').length);

  await expect(screen.getByTestId('speaker-enroll-file')).toHaveCount(1);
  await browser.evaluate(() => {
    const input = document.querySelector<HTMLInputElement>('[data-testid="speaker-enroll-file"]');
    if (!input) throw new Error('missing mounted enrollment file input');
    const transfer = new DataTransfer();
    const oversizedAudio = new Uint8Array(20 * 1024 * 1024 + 1);
    transfer.items.add(new File([oversizedAudio], 'too-large.wav', { type: 'audio/wav' }));
    Object.defineProperty(input, 'files', { configurable: true, value: transfer.files });
    input.dispatchEvent(new Event('change', { bubbles: true }));
    delete (input as { files?: FileList }).files;
  });
  await expect(screen.getByText(/所选音频超过 20 MiB|selected audio is larger than 20 MiB/)).toBeVisible();
  await expect(screen.getByTestId('speaker-enroll')).toBeDisabled();
  const enrollmentCallsAfterOversize = await browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.calls
    .filter(call => call.method === 'speaker.enroll').length);
  expect(enrollmentCallsAfterOversize).toBe(enrollmentCallsBefore);

  await configureFixture(browser, { enrollErrorCode: '@yovoice.error.speakerAudio' });
  await screen.getByTestId('speaker-enroll-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await screen.getByTestId('speaker-profile-name').fill('Rejected registration');
  await screen.getByTestId('speaker-enroll').tap();
  await expect(screen.getByText(/这段音频不能用于声纹识别|audio cannot be used for speaker recognition/)).toBeVisible();
  await expect(screen.getByTestId('speaker-enroll')).toBeEnabled();
  await expect(screen.getByTestId('speaker-profile-00000000000000000000000000000001')).toBeHidden();
  const enrollmentCallsAfterError = await browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.calls
    .filter(call => call.method === 'speaker.enroll').length);
  expect(enrollmentCallsAfterError).toBe(enrollmentCallsBefore + 1);

  await configureFixture(browser, { enrollErrorCode: null });
  await screen.getByTestId('speaker-profile-name').fill('Recovered registration');
  await screen.getByTestId('speaker-enroll').tap();
  await expect(screen.getByTestId('speaker-profile-00000000000000000000000000000001')).toBeVisible();
  const stateBeforeIdentifyErrors = await browser.evaluate(() => JSON.stringify(window.__yovoiceSyntheticSpeakerFixture.state));
  const profilesBeforeIdentifyErrors = await browser.evaluate(() => JSON.stringify(window.__yovoiceSyntheticSpeakerFixture.profiles));

  await screen.getByTestId('speaker-identify-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  const identifyCallsBefore = await browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.calls
    .filter(call => call.method === 'speaker.identify').length);
  await screen.getByTestId('speaker-identify').tap();
  await expect(screen.getByTestId('speaker-result')).toHaveAttribute('data-status', 'matched');
  const failures = [
    ['@yovoice.error.speakerRuntime', /声纹识别尚未准备好|Speaker recognition is not ready/],
    ['@yovoice.error.speakerFeature', /不兼容的识别版本|incompatible recognition version/],
    ['@yovoice.error.speakerBusy', /另一个声纹操作仍在进行|Another speaker operation is still running/],
  ] as const;
  for (const [code, message] of failures) {
    await configureFixture(browser, { identifyErrorCode: code });
    await screen.getByTestId('speaker-identify').tap();
    await expect(screen.getByText(message)).toBeVisible();
    await expect(screen.getByTestId('speaker-result')).toBeHidden();
    await expect(screen.getByTestId('speaker-identify')).toBeEnabled();
  }
  const identifyCallsAfterErrors = await browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.calls
    .filter(call => call.method === 'speaker.identify').length);
  expect(identifyCallsAfterErrors).toBe(identifyCallsBefore + 1 + failures.length);
  expect(await browser.evaluate(() => JSON.stringify(window.__yovoiceSyntheticSpeakerFixture.state))).toBe(stateBeforeIdentifyErrors);
  expect(await browser.evaluate(() => JSON.stringify(window.__yovoiceSyntheticSpeakerFixture.profiles))).toBe(profilesBeforeIdentifyErrors);

  await configureFixture(browser, { identifyErrorCode: null });
  await screen.getByTestId('speaker-identify').tap();
  await expect(screen.getByTestId('speaker-result')).toHaveAttribute('data-status', 'matched');
});

test('删除存储失败时确认对话框保留目标、反馈可见且可关闭后重试', async ({ app, browser, screen }) => {
  await openSpeakerPage(app, screen);
  await enrollBoundProfile(screen);
  await screen.getByTestId('speaker-enroll-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await screen.getByTestId('speaker-profile-name').fill('Fixture B');
  await screen.getByTestId('speaker-enroll').tap();
  const firstProfileID = '00000000000000000000000000000001';
  const secondProfileID = '00000000000000000000000000000002';
  await expect(screen.getByTestId(`speaker-profile-${secondProfileID}`)).toBeVisible();
  const voicesBefore = await browser.evaluate(() => JSON.stringify(window.__yovoiceSyntheticSpeakerFixture.state.voices));

  await configureFixture(browser, { deleteErrorCode: '@yovoice.error.speakerStorage' });
  await screen.getByTestId(`speaker-delete-${firstProfileID}`).tap();
  const confirm = screen.getByTestId('speaker-delete-confirm');
  await expect(confirm).toBeVisible();
  await confirm.tap();
  const dialog = screen.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('alert')).toContainText(/无法保存或读取声纹档案|could not be saved or read/);
  await expect(confirm).toBeVisible();
  await expect(screen.getByTestId(`speaker-profile-${firstProfileID}`)).toBeVisible();
  await expect(screen.getByTestId(`speaker-profile-${secondProfileID}`)).toBeVisible();

  await screen.getByRole('button', /取消|Cancel/).tap();
  await expect(confirm).toBeHidden();
  await screen.getByRole('button', /刷新|Refresh/).first().tap();
  await expect(screen.getByTestId(`speaker-profile-${firstProfileID}`)).toBeVisible();
  await expect(screen.getByTestId(`speaker-profile-${secondProfileID}`)).toBeVisible();

  await configureFixture(browser, { deleteErrorCode: null });
  await screen.getByTestId(`speaker-delete-${firstProfileID}`).tap();
  await screen.getByTestId('speaker-delete-confirm').tap();
  await expect(screen.getByTestId(`speaker-profile-${firstProfileID}`)).toBeHidden();
  await expect(screen.getByTestId(`speaker-profile-${secondProfileID}`)).toBeVisible();
  const deletionIDs = await browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.calls
    .filter(call => call.method === 'speaker.delete')
    .map(call => call.data.id));
  expect(deletionIDs).toEqual([firstProfileID, firstProfileID]);
  expect(await browser.evaluate(() => JSON.stringify(window.__yovoiceSyntheticSpeakerFixture.state.voices))).toBe(voicesBefore);
});

test('生成拒绝无绑定、失效绑定、缺参考原文、模式冲突和已删档案，且只在显式修正后恢复', async ({ app, browser, screen }) => {
  await serveSyntheticGeneratedOutput(browser);
  await openSpeakerPage(app, screen);
  const generationCallsBefore = await generationCallCount(browser);
  await expect(screen.getByTestId('speaker-generate')).toBeDisabled();
  await expect(screen.getByText(/识别结果不会在此自动选择档案|Identification never chooses a profile here/)).toBeVisible();

  await screen.getByTestId('speaker-enroll-file').setInputFiles('e2e/fixtures/speaker-fixture.wav');
  await screen.getByTestId('speaker-profile-name').fill('Unbound profile');
  await screen.getByTestId('speaker-enroll').tap();
  await expect(screen.getByTestId('speaker-profile-00000000000000000000000000000001')).toBeVisible();
  await enrollBoundProfile(screen);

  await screen.getByTestId('speaker-generation-profile').tap();
  await screen.getByRole('option', /Unbound profile/).tap();
  await expect(screen.getByText(/没有可用的绑定参考声音|no available bound reference voice/)).toBeVisible();
  await expect(screen.getByTestId('speaker-generate')).toBeDisabled();
  expect(await generationCallCount(browser)).toBe(generationCallsBefore);

  const invalidBindingID = '00000000000000000000000000000003';
  await browser.evaluate(id => {
    window.__yovoiceSyntheticSpeakerFixture.profiles.push({
      id,
      name: 'Missing bound voice',
      bindVoiceId: 'ffffffffffffffffffffffffffffffff',
      createdAt: '2026-10-12T00:00:00Z',
      modelSha256: 'synthetic-model',
      featureVersion: 'synthetic-version',
      dimension: 256,
    });
  }, invalidBindingID);
  await screen.getByRole('button', /刷新|Refresh/).first().tap();
  await screen.getByTestId('speaker-generation-profile').tap();
  await screen.getByRole('option', /Missing bound voice/).tap();
  await expect(screen.getByText(/没有可用的绑定参考声音|no available bound reference voice/)).toBeVisible();
  await expect(screen.getByTestId('speaker-generate')).toBeDisabled();
  expect(await generationCallCount(browser)).toBe(generationCallsBefore);

  const boundProfileID = '00000000000000000000000000000002';
  await screen.getByTestId('speaker-generation-profile').tap();
  await screen.getByRole('option', /Fixture A/).tap();
  await selectGenerationInputs(screen, 'Qwen3-TTS 1.7B Base');
  await browser.locator('#speaker-generation-text').fill('Keep this target text while correcting the reference.');
  await screen.getByTestId('speaker-generate').tap();
  await expect(screen.getByTestId('speaker-generated-output')).toBeVisible();

  await browser.evaluate(() => {
    window.__yovoiceSyntheticSpeakerFixture.state.voices[0].referenceText = '';
  });
  await selectGenerationInputs(screen, 'OmniVoice');
  const missingReferenceCloneMode = screen
    .getByTestId('speaker-omni-mode')
    .getByRole('radio', /^(音色克隆|Voice clone)$/);
  await missingReferenceCloneMode.tap();
  await expect(missingReferenceCloneMode).toHaveAttribute('aria-checked', 'true');

  // Omni 的绑定路径会强制 clone；缺失参考原文由 schema.Validate 拒绝并公开为
  // speakerRequestInvalid。fixture 只返回这一公开结果，不重写业务校验。
  await configureFixture(browser, { generateErrorCode: '@yovoice.error.speakerRequestInvalid' });
  const missingReferenceCallCount = await generationCallCount(browser);
  await screen.getByTestId('speaker-generate').tap();
  const missingReferenceRequest = await latestGenerationSnapshot(browser, {
    previousCallCount: missingReferenceCallCount,
    modelID: 'omnivoice-q8',
  });
  await expect(screen.getByText(/声纹档案请求无效|speaker profile request is invalid/)).toBeVisible();
  await expect(screen.getByTestId('speaker-generated-output')).toBeHidden();
  await expect(browser.locator('#speaker-generation-text')).toHaveValue('Keep this target text while correcting the reference.');
  expect(missingReferenceRequest.draft.voiceMode).toBe('clone');

  await browser.evaluate(() => {
    window.__yovoiceSyntheticSpeakerFixture.state.voices[0].referenceText = 'repaired reference';
  });
  await screen.getByTestId('speaker-omni-mode').getByRole('radio', /设计|Voice design/).tap();
  await configureFixture(browser, { generateErrorCode: '@yovoice.error.speakerClone' });
  await screen.getByTestId('speaker-generate').tap();
  await expect(screen.getByText(/所选档案或模型不能用于声音克隆|cannot be used for voice cloning/)).toBeVisible();
  await expect(screen.getByTestId('speaker-generated-output')).toBeHidden();

  const recoveredProfileID = '00000000000000000000000000000004';
  await browser.evaluate(deletedID => {
    const fixture = window.__yovoiceSyntheticSpeakerFixture;
    fixture.profiles.splice(fixture.profiles.findIndex(profile => profile.id === deletedID), 1);
  }, boundProfileID);
  await configureFixture(browser, { generateErrorCode: '@yovoice.error.speakerProfile' });
  await screen.getByTestId('speaker-generate').tap();
  await expect(screen.getByText(/该声纹档案已不可用|speaker profile is no longer available/)).toBeVisible();
  await expect(screen.getByTestId('speaker-generated-output')).toBeHidden();
  const staleGenerationProfileIDs = await browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.calls
    .filter(call => call.method === 'speaker.generate')
    .map(call => call.data.profileId));
  expect(staleGenerationProfileIDs).toEqual([boundProfileID, boundProfileID, boundProfileID, boundProfileID]);

  await browser.evaluate(replacementID => {
    const fixture = window.__yovoiceSyntheticSpeakerFixture;
    fixture.profiles.push({
      id: replacementID,
      name: 'Recovered bound profile',
      bindVoiceId: fixture.state.voices[0].id,
      createdAt: '2026-10-12T00:00:00Z',
      modelSha256: 'synthetic-model',
      featureVersion: 'synthetic-version',
      dimension: 256,
    });
  }, recoveredProfileID);
  await configureFixture(browser, { generateErrorCode: null });
  await screen.getByRole('button', /刷新|Refresh/).first().tap();
  await expect(screen.getByTestId('speaker-generate')).toBeDisabled();
  expect(await generationCallCount(browser)).toBe(generationCallsBefore + 4);

  await screen.getByTestId('speaker-generation-profile').tap();
  await screen.getByRole('option', /Recovered bound profile/).tap();
  const recoveredCloneMode = screen
    .getByTestId('speaker-omni-mode')
    .getByRole('radio', /^(音色克隆|Voice clone)$/);
  await recoveredCloneMode.tap();
  await expect(recoveredCloneMode).toHaveAttribute('aria-checked', 'true');
  const recoveryCallCount = await generationCallCount(browser);
  await screen.getByTestId('speaker-generate').tap();
  const recoveredRequest = await latestGenerationSnapshot(browser, {
    previousCallCount: recoveryCallCount,
    modelID: 'omnivoice-q8',
  });
  await expect(screen.getByTestId('speaker-generated-output')).toBeVisible();
  expect(recoveredRequest.draft.voiceMode).toBe('clone');
  const generationProfileIDs = await browser.evaluate(() => window.__yovoiceSyntheticSpeakerFixture.calls
    .filter(call => call.method === 'speaker.generate')
    .map(call => call.data.profileId));
  expect(generationProfileIDs).toEqual([
    boundProfileID,
    boundProfileID,
    boundProfileID,
    boundProfileID,
    recoveredProfileID,
  ]);
});
