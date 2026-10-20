import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { SegmentedControl, SegmentedControlItem } from '@astryxdesign/core/SegmentedControl';
import { Slider } from '@astryxdesign/core/Slider';
import { Switch } from '@astryxdesign/core/Switch';
import { TextArea } from '@astryxdesign/core/TextArea';
import { TextInput } from '@astryxdesign/core/TextInput';
import { useTranslator } from '@astryxdesign/core/i18n';
import { Mic, RefreshCw, Square, Trash2, Upload } from 'lucide-react';
import { call, CallError, isDesktop } from '../../shared/lib/client';
import { toBase64 } from '../../shared/lib/sound';
import { formatCallError } from '../../shared/i18n/format';
import { Selector } from '../../shared/selector';
import type { Draft, Generation, ModelPackage, Track, Voice } from '../../shared/workbench';
import type { SpeakerIdentification, SpeakerProfile, SpeakerRuntime, SpeakerSource } from '../../shared/speakers';
import { ConfirmDelete } from '../../shared/ui/app-dialog';
import { ModelOptions } from '../create/model-options';
import { modelSwitchPatch } from '../create/model-switch';
import { useTemporaryAudio } from '../media/use-temporary-audio';
import { useTemporaryRecorder } from '../media/use-temporary-recorder';
import { Player } from '../media/player';
import { speakerGenerationDraft } from './generation-draft';

const maxSpeakerInputBytes = 20 * 1024 * 1024;
const cancellationWindowMs = 5_000;
const cancellationAttemptMs = 1_500;
const cancellationRetryIntervalMs = 500;
const cancellationAttempts = 11;

type TemporaryInput =
  | { kind: 'file'; blob: Blob; label: string }
  | { kind: 'voice'; id: string; label: string };

type PendingOperation = {
  accepted: boolean;
  cancelConfirmed: boolean;
  cancelInFlight: boolean;
  requestId: string;
  kind: 'enroll' | 'identify' | 'generate';
  inputEpoch: number;
  cancelIntent: boolean;
};

type CancellationAttempt<T> =
  | { kind: 'response'; value: T }
  | { kind: 'error' }
  | { kind: 'timeout' };

function requestId() {
  return crypto.randomUUID().replaceAll('-', '');
}

async function waitForCancellationResponse<T>(request: Promise<T>, timeoutMs: number): Promise<CancellationAttempt<T>> {
  let timeout: ReturnType<typeof setTimeout> | undefined;
  const response = request.then(
    value => ({ kind: 'response' as const, value }),
    () => ({ kind: 'error' as const }),
  );
  const deadline = new Promise<CancellationAttempt<T>>(resolve => {
    timeout = setTimeout(() => resolve({ kind: 'timeout' }), timeoutMs);
  });
  const result = await Promise.race([response, deadline]);
  if (timeout) clearTimeout(timeout);
  return result;
}

function cloneCapable(model: ModelPackage) {
  if (model.family === 'index_tts2' || model.family === 'voxcpm2' || model.family === 'omnivoice') return true;
  return model.family === 'qwen3_tts' && (!model.variant || model.variant === 'base');
}

function profileCreatedAt(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? value : date.toLocaleString();
}

export function SpeakerPage({ voices, models, catalog, draft, runtimeReady, onGeneration }: {
  voices: Voice[];
  models: { id: string }[];
  catalog: ModelPackage[];
  draft: Draft;
  runtimeReady: boolean;
  onGeneration: (generation: Generation) => void;
}) {
  const t = useTranslator();
  const enrollInputRef = useRef<HTMLInputElement>(null);
  const identifyInputRef = useRef<HTMLInputElement>(null);
  const pendingRef = useRef<PendingOperation | null>(null);
  const mountedRef = useRef(false);
  const refreshRequestRef = useRef(0);
  const cancelPendingRef = useRef<() => Promise<void>>(async () => {});
  const [runtime, setRuntime] = useState<SpeakerRuntime | null>(null);
  const [profiles, setProfiles] = useState<SpeakerProfile[]>([]);
  const [listError, setListError] = useState('');
  const [profileListState, setProfileListState] = useState<'loading' | 'ready' | 'error'>('loading');
  const [enrollInput, setEnrollInput] = useState<TemporaryInput | null>(null);
  const [identifyInput, setIdentifyInput] = useState<TemporaryInput | null>(null);
  const [enrollName, setEnrollName] = useState('');
  const [bindVoiceId, setBindVoiceId] = useState('');
  const [identification, setIdentification] = useState<SpeakerIdentification | null>(null);
  const [notice, setNotice] = useState('');
  const [pending, setPending] = useState<PendingOperation | null>(null);
  const inputEpochRef = useRef(0);
  const [recordingTarget, setRecordingTarget] = useState<'enroll' | 'identify' | null>(null);
  const [recordingError, setRecordingError] = useState('');
  const [deleteTarget, setDeleteTarget] = useState<SpeakerProfile | null>(null);
  const [deleteError, setDeleteError] = useState('');
  const [deleting, setDeleting] = useState(false);
  const [generationProfileId, setGenerationProfileId] = useState('');
  const [generatedOutput, setGeneratedOutput] = useState<Generation | null>(null);
  const [generatedOutputError, setGeneratedOutputError] = useState('');
  const installedModels = useMemo(
    () => catalog.filter(model => models.some(installed => installed.id === model.id) && cloneCapable(model)),
    [catalog, models],
  );
  const generationSourceID = useRef(draft.id);
  const [generationDraft, setGenerationDraft] = useState(() => speakerGenerationDraft(draft, installedModels));
  const temporaryRecorder = useTemporaryRecorder({
    onComplete: ({ autoStopped, blob, durationSeconds, isCurrent }) => {
      if (!isCurrent()) return;
      const target = recordingTarget;
      setRecordingTarget(null);
      if (durationSeconds < 1) {
        setRecordingError(t('@yovoice.error.recordTooShort'));
        return;
      }
      if (target) {
        replaceInput(target, { kind: 'file', blob, label: t('@yovoice.speakers.recordedInput') });
        if (autoStopped) setNotice(t('@yovoice.speakers.recordingAutoStopped'));
      }
    },
    onError: error => {
      const name = error instanceof Error ? error.name : '';
      setRecordingTarget(null);
      if (name === 'NotAllowedError') setRecordingError(t('@yovoice.error.micDeniedMac'));
      else if (name === 'NotFoundError') setRecordingError(t('@yovoice.error.micNotFound'));
      else setRecordingError(t('@yovoice.error.micFailed', { detail: name || t('@yovoice.error.unknown') }));
    },
  });
  const { discard: discardTemporaryRecorder } = temporaryRecorder;
  const enrollPreview = useTemporaryAudio(enrollInput?.kind === 'file' ? enrollInput.blob : null);
  const identifyPreview = useTemporaryAudio(identifyInput?.kind === 'file' ? identifyInput.blob : null);
  const selectedGenerationProfile = profiles.find(profile => profile.id === generationProfileId);
  const selectedGenerationVoice = voices.find(voice => voice.id === selectedGenerationProfile?.bindVoiceId);
  const selectedGenerationModel = installedModels.find(model => model.id === generationDraft.modelId);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      void cancelPendingRef.current();
    };
  }, []);

  const refresh = useCallback(async () => {
    const request = refreshRequestRef.current + 1;
    refreshRequestRef.current = request;
    const canCommit = () => mountedRef.current && refreshRequestRef.current === request;

    if (!canCommit()) return;
    setListError('');
    setProfileListState('loading');
    if (!isDesktop) {
      if (!canCommit()) return;
      setRuntime({ configured: false });
      setListError(t('@yovoice.error.previewSpeakerUnavailable'));
      setProfileListState('error');
      return;
    }
    try {
      const [nextRuntime, nextProfiles] = await Promise.all([
        call<SpeakerRuntime>('speaker.runtime'),
        call<SpeakerProfile[]>('speaker.list'),
      ]);
      if (!canCommit()) return;
      setRuntime(nextRuntime);
      setProfiles(nextProfiles);
      setProfileListState('ready');
    } catch (error) {
      if (!canCommit()) return;
      setListError(formatCallError(t, error));
      setProfileListState('error');
    }
  }, [t]);

  useEffect(() => {
    void refresh();
    return discardTemporaryRecorder;
  }, [discardTemporaryRecorder, refresh]);

  useEffect(() => {
    if (generationSourceID.current === draft.id) return;
    generationSourceID.current = draft.id;
    setGenerationDraft(speakerGenerationDraft(draft, installedModels));
  }, [draft, installedModels]);

  const [recordingSeconds, setRecordingSeconds] = useState(0);
  useEffect(() => {
    if (!temporaryRecorder.recording || temporaryRecorder.startedAt === null) {
      setRecordingSeconds(0);
      return;
    }
    const updateElapsedTime = () => {
      setRecordingSeconds(Math.min(60, Math.floor((performance.now() - temporaryRecorder.startedAt!) / 1_000)));
    };
    updateElapsedTime();
    const timer = window.setInterval(updateElapsedTime, 250);
    return () => window.clearInterval(timer);
  }, [temporaryRecorder.recording, temporaryRecorder.startedAt]);

  function isCurrentOperation(operation: PendingOperation) {
    return mountedRef.current && pendingRef.current === operation;
  }

  function canUseOperationInput(operation: PendingOperation) {
    return isCurrentOperation(operation)
      && !operation.cancelIntent
      && inputEpochRef.current === operation.inputEpoch;
  }

  function acceptOperation(operation: PendingOperation) {
    if (!isCurrentOperation(operation)) return false;
    operation.accepted = true;
    return true;
  }

  function replaceInput(target: 'enroll' | 'identify', input: TemporaryInput | null) {
    stopRecorder();
    if (pendingRef.current) void cancelPending();
    const epoch = inputEpochRef.current + 1;
    inputEpochRef.current = epoch;
    setNotice('');
    setIdentification(null);
    if (target === 'enroll') setEnrollInput(input);
    else setIdentifyInput(input);
  }

  function chooseFile(target: 'enroll' | 'identify', file: File | undefined) {
    if (!file) return;
    if (file.size > maxSpeakerInputBytes) {
      setNotice(t('@yovoice.error.speakerInputTooLarge'));
      return;
    }
    replaceInput(target, { kind: 'file', blob: file, label: file.name });
  }

  function chooseVoice(target: 'enroll' | 'identify', voiceId: string) {
    const voice = voices.find(item => item.id === voiceId);
    replaceInput(target, voice ? { kind: 'voice', id: voice.id, label: voice.name } : null);
  }

  function stopRecorder() {
    discardTemporaryRecorder();
    setRecordingTarget(null);
  }

  async function record(target: 'enroll' | 'identify') {
    setRecordingError('');
    if (temporaryRecorder.active) {
      temporaryRecorder.stop();
      return;
    }
    setNotice('');
    setRecordingTarget(target);
    await temporaryRecorder.start();
  }

  async function sourceFor(input: TemporaryInput): Promise<SpeakerSource> {
    if (input.kind === 'voice') return { voiceId: input.id };
    if (input.blob.size > maxSpeakerInputBytes) throw new CallError('@yovoice.error.speakerInputTooLarge');
    return { base64: await toBase64(input.blob) };
  }

  async function begin(kind: PendingOperation['kind'], action: (operation: PendingOperation) => Promise<void>) {
    if (pendingRef.current) return;
    const operation: PendingOperation = {
      accepted: false,
      cancelConfirmed: false,
      cancelInFlight: false,
      requestId: requestId(),
      kind,
      inputEpoch: inputEpochRef.current,
      cancelIntent: false,
    };
    pendingRef.current = operation;
    setPending(operation);
    setNotice('');
    try {
      await action(operation);
    } catch (error) {
      if (isCurrentOperation(operation)) {
        if (operation.cancelIntent && error instanceof CallError && error.code === '@yovoice.error.speakerCancelled') {
          setNotice(t('@yovoice.speakers.cancelRequested'));
          return;
        }
        setNotice(formatCallError(t, error));
        if (error instanceof CallError && error.code === '@yovoice.error.speakerTimeout') {
          // 客户端无法得知超时前请求是否已经完成；只对自己的 requestId 发送取消，
          // 随后刷新档案列表，不自动重发原操作。
          await cancelPending();
          await refresh();
        }
      }
    } finally {
      if (isCurrentOperation(operation)) {
        pendingRef.current = null;
        setPending(null);
      }
    }
  }

  async function enroll() {
    if (!enrollInput) {
      setNotice(t('@yovoice.speakers.inputRequired'));
      return;
    }
    await begin('enroll', async operation => {
      const source = await sourceFor(enrollInput);
      if (!canUseOperationInput(operation)) return;
      const profile = await call<SpeakerProfile>('speaker.enroll', {
        requestId: operation.requestId,
        source,
        ...(enrollName === '' ? {} : { name: enrollName }),
        ...(bindVoiceId ? { bindVoiceId } : {}),
      });
      if (!acceptOperation(operation)) return;
      setNotice(t('@yovoice.speakers.enrolled', { name: profile.name }));
      await refresh();
    });
  }

  async function identify() {
    if (!identifyInput) {
      setNotice(t('@yovoice.speakers.inputRequired'));
      return;
    }
    setIdentification(null);
    await begin('identify', async operation => {
      const source = await sourceFor(identifyInput);
      if (!canUseOperationInput(operation)) return;
      const result = await call<SpeakerIdentification>('speaker.identify', {
        requestId: operation.requestId,
        source,
      });
      if (!canUseOperationInput(operation)) return;
      setIdentification(result);
    });
  }

  async function generate() {
    if (!generationProfileId) {
      setNotice(t('@yovoice.speakers.profileRequired'));
      return;
    }
    // 这块播放器只代表当前页面最近一次成功的声纹生成。新请求已开始时，
    // 不能把旧结果误标为本次成功；已保存的生成历史仍由 Workbench 管理。
    setGeneratedOutput(null);
    setGeneratedOutputError('');
    await begin('generate', async operation => {
      const wireDraft = {
        ...speakerGenerationDraft(generationDraft, installedModels),
      };
      const generation = await call<Generation>('speaker.generate', { requestId: operation.requestId, profileId: generationProfileId, draft: wireDraft });
      if (!acceptOperation(operation)) return;
      setGeneratedOutput(generation);
      setGeneratedOutputError('');
      onGeneration(generation);
      setNotice(t('@yovoice.speakers.generated'));
    });
  }

  async function cancelPending() {
    const operation = pendingRef.current;
    if (!operation || operation.accepted || operation.cancelConfirmed || operation.cancelInFlight) return;
    operation.cancelIntent = true;
    operation.cancelInFlight = true;
    if (mountedRef.current) setPending({ ...operation });
    const deadline = performance.now() + cancellationWindowMs;
    for (let attempt = 0; attempt < cancellationAttempts; attempt += 1) {
      if (pendingRef.current !== operation || operation.accepted || operation.cancelConfirmed) return;
      const remaining = deadline - performance.now();
      if (remaining <= 0) break;
      const response = await waitForCancellationResponse(
        call<{ cancelRequested: boolean }>('speaker.cancel', { requestId: operation.requestId }),
        Math.min(cancellationAttemptMs, remaining),
      );
      if (pendingRef.current !== operation || operation.accepted || operation.cancelConfirmed) return;
      if (response.kind === 'response' && response.value.cancelRequested) {
        operation.cancelConfirmed = true;
        operation.cancelInFlight = false;
        if (mountedRef.current) setNotice(t('@yovoice.speakers.cancelRequested'));
        return;
      }
      const retryDelay = Math.min(cancellationRetryIntervalMs, deadline - performance.now());
      if (retryDelay > 0) await new Promise(resolve => setTimeout(resolve, retryDelay));
    }
    if (pendingRef.current === operation && !operation.accepted) {
      operation.cancelInFlight = false;
      if (mountedRef.current) {
        setPending({ ...operation });
        setNotice(t('@yovoice.speakers.cancelUnconfirmed'));
      }
    }
  }
  cancelPendingRef.current = cancelPending;

  async function removeProfile() {
    const target = deleteTarget;
    if (!target) return;
    setDeleteError('');
    setDeleting(true);
    try {
      await call('speaker.delete', { id: target.id });
      setProfiles(current => current.filter(profile => profile.id !== target.id));
      setDeleteError('');
      setDeleteTarget(null);
      setGenerationProfileId(current => current === target.id ? '' : current);
      await refresh();
    } catch (error) {
      setDeleteError(formatCallError(t, error));
    } finally {
      setDeleting(false);
    }
  }

  function openDelete(profile: SpeakerProfile) {
    setDeleteError('');
    setDeleteTarget(profile);
  }

  function closeDelete() {
    setDeleteError('');
    setDeleteTarget(null);
  }

  const profileOptions = profiles.map(profile => ({ value: profile.id, label: `${profile.name} · ${profile.id.slice(0, 8)}` }));
  const voiceOptions = [{ value: '', label: t('@yovoice.speakers.noVoice') }, ...voices.map(voice => ({ value: voice.id, label: voice.name }))];
  const isPending = !!pending;

  return <VStack className="speakers-page" gap={6}>
    <HStack className="speakers-heading" gap={3} vAlign="center" wrap="wrap">
      <VStack gap={1}><h1>{t('@yovoice.speakers.title')}</h1><p className="muted">{t('@yovoice.speakers.description')}</p></VStack>
      <Button label={t('@yovoice.speakers.refresh')} size="sm" icon={<RefreshCw size={16} />} isLoading={false} onClick={() => void refresh()} />
    </HStack>

    {!runtime?.configured ? (
      <p className="speaker-runtime" role="alert">
        {t('@yovoice.speakers.runtimeUnavailable')}
      </p>
    ) : (
      <p className="speaker-runtime" data-testid="speaker-runtime" role="status">
        {t('@yovoice.speakers.runtimeReady', {
          threshold: runtime.threshold,
          margin: runtime.margin,
          timeout: runtime.timeoutSeconds,
        })}
      </p>
    )}
    {notice ? <p className="speaker-notice" role="status">{notice}</p> : null}
    {recordingError ? <p className="speaker-notice" role="alert">{recordingError}</p> : null}

    <VStack className="speaker-workspace" gap={6}>
      <SpeakerInputPanel title={t('@yovoice.speakers.enrollTitle')} target="enroll" input={enrollInput} voices={voices} selectedVoiceId={enrollInput?.kind === 'voice' ? enrollInput.id : ''}
        inputRef={enrollInputRef} recording={recordingTarget === 'enroll' && temporaryRecorder.recording} playbackSuspended={recordingTarget !== null} recordingSeconds={recordingSeconds} pending={isPending} preview={enrollPreview}
        onChooseVoice={id => chooseVoice('enroll', id)} onRecord={() => void record('enroll')} onClear={() => replaceInput('enroll', null)} />
      <VStack className="speaker-form" gap={3}>
        <TextInput id="speaker-profile-name" data-testid="speaker-profile-name" label={t('@yovoice.speakers.name')} value={enrollName} onChange={setEnrollName} />
        <Selector data-testid="speaker-profile-bind-voice" label={t('@yovoice.speakers.bindVoice')} value={bindVoiceId} options={voiceOptions} onChange={setBindVoiceId} />
        <HStack gap={2} wrap="wrap"><Button data-testid="speaker-enroll" label={t('@yovoice.speakers.enroll')} variant="primary" isDisabled={!enrollInput || isPending || !runtime?.configured} isLoading={pending?.kind === 'enroll'} onClick={() => void enroll()} />{pending?.kind === 'enroll' ? <Button label={t('@yovoice.speakers.cancel')} variant="secondary" onClick={() => void cancelPending()} /> : null}</HStack>
      </VStack>

      <SpeakerInputPanel title={t('@yovoice.speakers.identifyTitle')} target="identify" input={identifyInput} voices={voices} selectedVoiceId={identifyInput?.kind === 'voice' ? identifyInput.id : ''}
        inputRef={identifyInputRef} recording={recordingTarget === 'identify' && temporaryRecorder.recording} playbackSuspended={recordingTarget !== null} recordingSeconds={recordingSeconds} pending={isPending} preview={identifyPreview}
        onChooseVoice={id => chooseVoice('identify', id)} onRecord={() => void record('identify')} onClear={() => replaceInput('identify', null)} />
      <HStack gap={2} wrap="wrap"><Button data-testid="speaker-identify" label={t('@yovoice.speakers.identify')} variant="primary" isDisabled={!identifyInput || isPending || !runtime?.configured} isLoading={pending?.kind === 'identify'} onClick={() => void identify()} />{pending?.kind === 'identify' ? <Button label={t('@yovoice.speakers.cancel')} variant="secondary" onClick={() => void cancelPending()} /> : null}</HStack>
      {identification ? <IdentificationResult result={identification} profiles={profiles} /> : null}
    </VStack>

    <VStack data-testid="speaker-profiles" data-state={profileListState} className="speaker-profiles" gap={3}>
      <HStack hAlign="between" vAlign="center" gap={2} wrap="wrap"><h2>{t('@yovoice.speakers.profiles')}</h2><Button label={t('@yovoice.speakers.refresh')} size="sm" icon={<RefreshCw size={16} />} onClick={() => void refresh()} /></HStack>
      {listError ? <p role="alert">{listError}</p> : null}
      {!listError && profiles.length === 0 ? <p className="empty-state">{t('@yovoice.speakers.emptyProfiles')}</p> : null}
      {profiles.map(profile => (
        <HStack
          key={profile.id}
          className="speaker-profile-row"
          data-testid={`speaker-profile-${profile.id}`}
          data-profile-id={profile.id}
          data-speaker-profile-row="true"
          gap={3}
          vAlign="center"
          wrap="wrap"
        >
          <VStack className="speaker-profile-meta" gap={1}>
            <strong>{profile.name}</strong>
            <small>{profile.id}</small>
            <small>{t('@yovoice.speakers.profileCreatedAt', { date: profileCreatedAt(profile.createdAt) })}</small>
            <small>{t('@yovoice.speakers.profileVersion', { version: profile.featureVersion })}</small>
            <small>{profile.bindVoiceId ? t('@yovoice.speakers.bound', { id: profile.bindVoiceId }) : t('@yovoice.speakers.unbound')}</small>
          </VStack>
          <Button data-testid={`speaker-delete-${profile.id}`} label={t('@yovoice.speakers.deleteProfile', { name: profile.name })} isIconOnly icon={<Trash2 size={16} />} variant="ghost" onClick={() => openDelete(profile)} />
        </HStack>
      ))}
    </VStack>

    <VStack className="speaker-generation" gap={3}>
      <h2>{t('@yovoice.speakers.generateTitle')}</h2>
      <Selector data-testid="speaker-generation-profile" label={t('@yovoice.speakers.chooseProfile')} value={generationProfileId} placeholder={t('@yovoice.speakers.chooseProfile')} options={profileOptions} onChange={setGenerationProfileId} />
      {selectedGenerationProfile ? selectedGenerationVoice
        ? <BoundReference voice={selectedGenerationVoice} recording={recordingTarget !== null} onError={message => setNotice(formatCallError(t, message))} />
        : <p className="muted">{t('@yovoice.speakers.boundVoiceMissing')}</p>
        : <p className="muted">{t('@yovoice.speakers.chooseProfileHint')}</p>}
      <Selector
        data-testid="speaker-generation-model"
        label={t('@yovoice.speakers.model')}
        value={generationDraft.modelId}
        options={installedModels.map(model => ({ value: model.id, label: model.name }))}
        onChange={modelId => setGenerationDraft(current => ({
          ...current,
          ...modelSwitchPatch(current, installedModels, modelId),
        }))}
      />
      <label htmlFor="speaker-generation-text">{t('@yovoice.speakers.targetText')}</label>
      <textarea id="speaker-generation-text" className="speaker-text" value={generationDraft.text} maxLength={12000} onChange={event => setGenerationDraft(current => ({ ...current, text: event.target.value }))} />
      <SpeakerGenerationSettings draft={generationDraft} model={selectedGenerationModel} voices={voices} boundVoice={selectedGenerationVoice} onChange={patch => setGenerationDraft(current => ({ ...current, ...patch }))} />
      <HStack gap={2} wrap="wrap"><Button data-testid="speaker-generate" label={t('@yovoice.speakers.generate')} variant="primary" isDisabled={isPending || !runtimeReady || !selectedGenerationVoice || !generationDraft.text.trim() || !installedModels.some(model => model.id === generationDraft.modelId)} isLoading={pending?.kind === 'generate'} onClick={() => void generate()} />{pending?.kind === 'generate' ? <Button label={t('@yovoice.speakers.cancel')} variant="secondary" onClick={() => void cancelPending()} /> : null}</HStack>
      {!runtimeReady || installedModels.length === 0 ? <p role="status">{t('@yovoice.speakers.generationUnavailable')}</p> : null}
      {generatedOutput ? <GeneratedOutput output={generatedOutput} recording={recordingTarget !== null} onError={message => setGeneratedOutputError(formatCallError(t, message))} /> : null}
      {generatedOutputError ? <p className="speaker-notice" role="alert">{generatedOutputError}</p> : null}
    </VStack>
    <input data-testid="speaker-enroll-file" hidden ref={enrollInputRef} type="file" accept="audio/*,.aac,.m4a,.mp3,.wav,.flac,.ogg,.opus,.aiff,.aif,.wma,.webm" onChange={event => { chooseFile('enroll', event.target.files?.[0]); event.target.value = ''; }} />
    <input data-testid="speaker-identify-file" hidden ref={identifyInputRef} type="file" accept="audio/*,.aac,.m4a,.mp3,.wav,.flac,.ogg,.opus,.aiff,.aif,.wma,.webm" onChange={event => { chooseFile('identify', event.target.files?.[0]); event.target.value = ''; }} />
    {deleteTarget ? (
      <ConfirmDelete
        title={t('@yovoice.speakers.deleteTitle')}
        description={t('@yovoice.speakers.deleteBody', { name: deleteTarget.name })}
        confirmLabel={t('@yovoice.speakers.deleteConfirm')}
        confirmTestId="speaker-delete-confirm"
        busy={deleting}
        error={deleteError}
        onClose={closeDelete}
        onConfirm={() => void removeProfile()}
      />
    ) : null}
  </VStack>;
}

function BoundReference({ voice, recording, onError }: { voice: Voice; recording: boolean; onError: (message: string) => void }) {
  const t = useTranslator();
  const track: Track = {
    id: voice.id,
    name: voice.name,
    fileName: voice.fileName,
    kind: 'voices',
    subtitle: t('@yovoice.app.subtitleReference'),
  };

  return <VStack data-testid="speaker-bound-reference" className="speaker-input-summary" gap={2}>
    <p className="muted">{t('@yovoice.speakers.boundVoiceReadOnly', { name: voice.name })}</p>
    <p><strong>{t('@yovoice.speakers.boundReferenceText')}</strong> {voice.referenceText || t('@yovoice.speakers.boundReferenceTextEmpty')}</p>
    <Player compact track={track} suspended={recording} onError={onError} />
  </VStack>;
}

const emotionKeys = ['happy', 'angry', 'sad', 'afraid', 'disgusted', 'melancholy', 'surprised', 'calm'] as const;

function SpeakerGenerationSettings({ draft, model, voices, boundVoice, onChange }: {
  draft: Draft;
  model: ModelPackage | undefined;
  voices: Voice[];
  boundVoice: Voice | undefined;
  onChange: (patch: Partial<Draft>) => void;
}) {
  const t = useTranslator();
  if (!model) return null;

  return <VStack data-testid="speaker-generation-settings" className="speaker-generation-settings" gap={3}>
    <h3>{t('@yovoice.speakers.generationSettings')}</h3>
    {model.family === 'index_tts2' ? <IndexSpeakerSettings draft={draft} model={model} voices={voices} onChange={onChange} /> : null}
    {model.family === 'qwen3_tts' ? <QwenSpeakerSettings draft={draft} onChange={onChange} /> : null}
    {model.family === 'omnivoice' ? <OmniSpeakerSettings draft={draft} boundVoice={boundVoice} onChange={onChange} /> : null}
    {model.family === 'voxcpm2' ? <VoxSpeakerSettings draft={draft} boundVoice={boundVoice} onChange={onChange} /> : null}
  </VStack>;
}

function IndexSpeakerSettings({ draft, model, voices, onChange }: {
  draft: Draft;
  model: ModelPackage;
  voices: Voice[];
  onChange: (patch: Partial<Draft>) => void;
}) {
  const t = useTranslator();
  const languageOptions = (model.id.startsWith('index-2.5')
    ? ['zh', 'en', 'ja', 'es', 'ar']
    : ['zh', 'en']
  ).map(value => ({ value, label: t(`@yovoice.language.${value}`) }));
  const emotionVoices = [
    { value: '', label: t('@yovoice.speakers.noVoice') },
    ...voices.map(voice => ({ value: voice.id, label: voice.name })),
  ];

  return <>
    <SegmentedControl data-testid="speaker-index-expression" label={t('@yovoice.create.expression')} size="sm" layout="fill" value={draft.mode} onChange={mode => onChange({ mode: mode as Draft['mode'] })}>
      {['speaker', 'reference', 'vector', 'text'].map(mode => <SegmentedControlItem key={mode} value={mode} label={t(`@yovoice.create.mode.${mode}`)} />)}
    </SegmentedControl>
    {draft.mode === 'reference' ? <Selector data-testid="speaker-index-emotion-voice" label={t('@yovoice.create.mode.reference')} value={draft.emotionVoiceId ?? ''} options={emotionVoices} onChange={emotionVoiceId => onChange({ emotionVoiceId: emotionVoiceId || null })} /> : null}
    {draft.mode === 'text' ? <VStack gap={3}>
      <TextArea label={t('@yovoice.create.emotionDescription')} value={draft.emotionText} onChange={emotionText => onChange({ emotionText: emotionText.slice(0, 500) })} placeholder={t('@yovoice.create.emotionPlaceholder')} rows={3} isDisabled={draft.inferEmotion} />
      <Switch label={t('@yovoice.create.inferEmotion')} size="sm" value={draft.inferEmotion} onChange={inferEmotion => onChange({ inferEmotion })} labelPosition="start" labelSpacing="spread" />
    </VStack> : null}
    {draft.mode === 'vector' ? <EmotionVectorControls values={draft.emotions} onChange={emotions => onChange({ emotions })} /> : null}
    {draft.mode === 'text' || draft.mode === 'vector' ? <Switch label={t('@yovoice.create.randomEmotion')} size="sm" value={draft.randomEmotion} onChange={randomEmotion => onChange({ randomEmotion })} labelPosition="start" labelSpacing="spread" /> : null}
    {draft.mode !== 'speaker' ? <Slider label={t('@yovoice.create.emotionStrength')} value={draft.emotionStrength} min={0} max={1} step={0.05} onChange={(emotionStrength: number) => onChange({ emotionStrength })} valueDisplay="text" formatValue={value => `${Math.round(value * 100)}%`} /> : null}
    <Selector data-testid="speaker-index-language" label={t('@yovoice.create.language')} value={draft.language} options={languageOptions} onChange={language => onChange({ language })} />
    <Slider label={t('@yovoice.create.speed')} value={draft.speed} min={0.5} max={2} step={0.05} onChange={(speed: number) => onChange({ speed })} valueDisplay="text" formatValue={value => `${value.toFixed(2)}×`} />
    <ModelOptions draft={draft} family="index_tts2" change={onChange} />
  </>;
}

function EmotionVectorControls({ values, onChange }: { values: number[]; onChange: (values: number[]) => void }) {
  const t = useTranslator();
  return <VStack data-testid="speaker-index-emotion-vector" gap={3}>
    {emotionKeys.map((key, index) => <Slider key={key} label={t(`@yovoice.emotion.${key}`)} value={values[index] ?? 0} min={0} max={1} step={0.05} onChange={(value: number) => {
      const next = [...values];
      next[index] = value;
      onChange(next);
    }} valueDisplay="text" formatValue={value => `${Math.round(value * 100)}%`} />)}
  </VStack>;
}

function QwenSpeakerSettings({ draft, onChange }: { draft: Draft; onChange: (patch: Partial<Draft>) => void }) {
  const t = useTranslator();
  const languageOptions = ['auto', 'zh', 'en', 'ja', 'ko', 'de', 'fr', 'ru', 'pt', 'es', 'it'].map(value => ({
    value,
    label: value === 'auto' ? t('@yovoice.reference.auto') : t(`@yovoice.language.${value}`),
  }));

  return <>
    <Selector data-testid="speaker-qwen-language" label={t('@yovoice.create.language')} value={draft.synthesisLanguage || 'auto'} options={languageOptions} onChange={synthesisLanguage => onChange({ synthesisLanguage })} />
    <ModelOptions draft={draft} family="qwen3_tts" change={onChange} />
  </>;
}

function OmniSpeakerSettings({ draft, boundVoice, onChange }: { draft: Draft; boundVoice: Voice | undefined; onChange: (patch: Partial<Draft>) => void }) {
  const t = useTranslator();
  const mode = draft.voiceMode ?? '';
  const automaticMode = boundVoice ? t('@yovoice.vox.clone') : t('@yovoice.reference.auto');

  return <>
    <SegmentedControl data-testid="speaker-omni-mode" label={t('@yovoice.vox.generationMode')} size="sm" layout="fill" value={mode} onChange={voiceMode => onChange({ voiceMode: voiceMode ? voiceMode as Draft['voiceMode'] : undefined })}>
      <SegmentedControlItem value="" label={`${t('@yovoice.reference.auto')} · ${automaticMode}`} />
      <SegmentedControlItem value="design" label={t('@yovoice.vox.design')} />
      <SegmentedControlItem value="clone" label={t('@yovoice.vox.clone')} />
    </SegmentedControl>
    <label className="number-field">{t('@yovoice.reference.omniLanguage')}<input data-testid="speaker-omni-language" value={draft.synthesisLanguage ?? ''} maxLength={32} placeholder="auto" onChange={event => onChange({ synthesisLanguage: event.target.value })} /></label>
    <Slider label={t('@yovoice.create.speed')} value={draft.omniSpeed || 1} min={0.5} max={2} step={0.05} onChange={(omniSpeed: number) => onChange({ omniSpeed })} valueDisplay="text" formatValue={value => `${value.toFixed(2)}×`} />
    <ModelOptions draft={draft} family="omnivoice" change={onChange} />
  </>;
}

function VoxSpeakerSettings({ draft, boundVoice, onChange }: { draft: Draft; boundVoice: Voice | undefined; onChange: (patch: Partial<Draft>) => void }) {
  const t = useTranslator();
  const mode = draft.voxMode ?? '';
  const automaticMode = boundVoice?.referenceText ? 'continuation' : 'clone';
  const effectiveMode = mode || automaticMode;

  return <>
    <SegmentedControl data-testid="speaker-vox-mode" label={t('@yovoice.vox.generationMode')} size="sm" layout="fill" value={mode} onChange={voxMode => onChange({ voxMode: voxMode as Draft['voxMode'] })}>
      <SegmentedControlItem value="" label={`${t('@yovoice.reference.auto')} · ${t(`@yovoice.vox.${automaticMode}`)}`} />
      <SegmentedControlItem value="design" label={t('@yovoice.vox.design')} />
      <SegmentedControlItem value="clone" label={t('@yovoice.vox.clone')} />
      <SegmentedControlItem value="continuation" label={t('@yovoice.vox.continuation')} />
    </SegmentedControl>
    {effectiveMode !== 'continuation' ? <TextArea label={effectiveMode === 'design' ? t('@yovoice.vox.voiceDescription') : t('@yovoice.vox.styleGuidance')} value={draft.voiceDescription ?? ''} onChange={voiceDescription => onChange({ voiceDescription: voiceDescription.slice(0, 500) })} placeholder={effectiveMode === 'design' ? t('@yovoice.vox.voiceDescriptionPlaceholder') : t('@yovoice.vox.stylePlaceholder')} rows={3} /> : null}
    <Slider label={t('@yovoice.vox.guidanceScale')} value={draft.guidanceScale || 2} min={0.5} max={5} step={0.1} onChange={(guidanceScale: number) => onChange({ guidanceScale })} valueDisplay="text" formatValue={value => value.toFixed(1)} />
    <label className="number-field">{t('@yovoice.vox.inferenceSteps')}<input type="number" min={1} max={50} value={draft.inferenceSteps || 10} onChange={event => { if (event.target.value) onChange({ inferenceSteps: Number(event.target.value) }); }} /></label>
    <ModelOptions draft={draft} family="voxcpm2" change={onChange} />
  </>;
}

function GeneratedOutput({ output, recording, onError }: { output: Generation; recording: boolean; onError: (message: string) => void }) {
  const t = useTranslator();
  const track: Track = {
    id: output.id,
    name: output.title,
    fileName: output.fileName,
    kind: 'outputs',
    subtitle: t('@yovoice.app.subtitleOutput'),
  };

  return <VStack data-testid="speaker-generated-output" data-output-id={output.id} className="speaker-generated-output" gap={2}>
    <h3>{t('@yovoice.speakers.generatedOutput')}</h3>
    <Player track={track} suspended={recording} onError={onError} />
  </VStack>;
}

function SpeakerInputPanel({ title, target, input, voices, selectedVoiceId, inputRef, recording, playbackSuspended, recordingSeconds, pending, preview, onChooseVoice, onRecord, onClear }: {
  title: string;
  target: 'enroll' | 'identify';
  input: TemporaryInput | null;
  voices: Voice[];
  selectedVoiceId: string;
  inputRef: React.RefObject<HTMLInputElement | null>;
  recording: boolean;
  playbackSuspended: boolean;
  recordingSeconds: number;
  pending: boolean;
  preview: string;
  onChooseVoice: (id: string) => void;
  onRecord: () => void;
  onClear: () => void;
}) {
  const t = useTranslator();
  const previewAudio = useRef<HTMLAudioElement>(null);

  useEffect(() => {
    if (playbackSuspended) previewAudio.current?.pause();
  }, [playbackSuspended]);

  function preventPreviewPlayback(event: React.SyntheticEvent<HTMLAudioElement>) {
    if (playbackSuspended) event.currentTarget.pause();
  }

  return <VStack className="speaker-input-panel" gap={3}>
    <h2>{title}</h2>
    <HStack gap={2} wrap="wrap"><Button label={t('@yovoice.speakers.chooseFile')} icon={<Upload size={16} />} isDisabled={pending} onClick={() => inputRef.current?.click()} /><Button data-testid={`speaker-record-${target}`} label={recording ? t('@yovoice.speakers.stopRecord') : t('@yovoice.speakers.record')} icon={recording ? <Square size={16} /> : <Mic size={16} />} variant="secondary" isDisabled={pending} onClick={onRecord} /></HStack>
    {recording ? <p className="speaker-recording-status" role="status">{t('@yovoice.speakers.recordingStatus', { seconds: recordingSeconds, limit: 60 })}</p> : null}
    <Selector label={t('@yovoice.speakers.existingVoice')} value={selectedVoiceId} options={[{ value: '', label: t('@yovoice.speakers.noInput') }, ...voices.map(voice => ({ value: voice.id, label: voice.name }))]} onChange={onChooseVoice} />
    {input ? <HStack className="speaker-input-summary" gap={2} vAlign="center" wrap="wrap">
      <strong>{input.label}</strong>
      <small>{input.kind === 'file' ? t('@yovoice.speakers.temporaryInput') : t('@yovoice.speakers.voiceInput')}</small>
      {input.kind === 'file' && preview ? <audio
        controls
        aria-disabled={playbackSuspended}
        aria-label={t('@yovoice.speakers.previewInput', { target })}
        data-testid={`speaker-input-preview-${target}`}
        ref={previewAudio}
        src={preview}
        tabIndex={playbackSuspended ? -1 : 0}
        onPlay={preventPreviewPlayback}
      /> : null}
      <Button label={t('@yovoice.speakers.clearInput')} size="sm" variant="ghost" onClick={onClear} />
    </HStack> : null}
  </VStack>;
}

function IdentificationResult({ result, profiles }: { result: SpeakerIdentification; profiles: SpeakerProfile[] }) {
  const t = useTranslator();
  const matched = result.profileId ? profiles.find(profile => profile.id === result.profileId) : null;
  return <VStack
    data-testid="speaker-result"
    data-candidate-count={result.candidates.length}
    data-margin={result.margin}
    data-profile-id={result.profileId ?? ''}
    data-score={result.score ?? ''}
    data-status={result.status}
    data-threshold={result.threshold}
    className="speaker-result"
    gap={2}
    role="status"
  >
    <h2>{t(`@yovoice.speakers.result.${result.status}`)}</h2>
    <p>{t('@yovoice.speakers.resultReason', { reason: result.reason })}</p>
    {matched ? <p>{t('@yovoice.speakers.matchedProfile', { name: matched.name, id: matched.id })}</p> : null}
    {result.score !== undefined ? <p>{t('@yovoice.speakers.score', { score: result.score })}</p> : null}
    <p>{t('@yovoice.speakers.parameters', { threshold: result.threshold, margin: result.margin, version: result.featureVersion })}</p>
    {result.candidates.length ? (
      <VStack gap={1}>
        {result.candidates.map(candidate => (
          <small
            key={candidate.profileId}
            data-speaker-candidate-id={candidate.profileId}
            data-testid={`speaker-candidate-${candidate.profileId}`}
          >
            {candidate.name} · {candidate.profileId} · {candidate.score}
          </small>
        ))}
      </VStack>
    ) : <p>{t('@yovoice.speakers.noCandidates')}</p>}
  </VStack>;
}
