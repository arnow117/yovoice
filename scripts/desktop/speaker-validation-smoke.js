return (async () => {
  const input = id => document.querySelector(`[data-testid="${id}"]`);
  const interactiveControl = id => {
    const control = input(id);
    if (!control) return null;

    const selector = 'button, input, select, textarea, [role="button"]';
    return control.matches?.(selector) ? control : control.querySelector?.(selector);
  };
  const intersect = (first, second) => {
    const left = Math.max(first.left, second.left);
    const right = Math.min(first.right, second.right);
    const top = Math.max(first.top, second.top);
    const bottom = Math.min(first.bottom, second.bottom);
    return {
      left,
      right,
      top,
      bottom,
      width: Math.max(0, right - left),
      height: Math.max(0, bottom - top),
    };
  };
  const bounds = rect => ({
    left: rect.left,
    right: rect.right,
    top: rect.top,
    bottom: rect.bottom,
    width: rect.width,
    height: rect.height,
  });
  const publicElementLabel = element => {
    const testID = element?.dataset?.testid;
    const safeTestID = typeof testID === 'string' && /^[A-Za-z0-9_-]{1,80}$/.test(testID)
      ? testID
      : null;
    const className = typeof element?.className === 'string'
      ? element.className
        .split(/\s+/)
        .filter(name => /^[A-Za-z0-9_-]{1,80}$/.test(name))
        .slice(0, 3)
      : [];
    return { testID: safeTestID, className };
  };
  const numericBounds = rect => ({
    left: Math.round(rect.left),
    right: Math.round(rect.right),
    top: Math.round(rect.top),
    bottom: Math.round(rect.bottom),
    width: Math.round(rect.width),
    height: Math.round(rect.height),
  });
  const presentation = style => ({
    display: style?.display ?? 'unavailable',
    visibility: style?.visibility ?? 'unavailable',
    opacity: style?.opacity ?? 'unavailable',
  });
  const isHidden = style => style?.display === 'none'
    || style?.visibility === 'hidden'
    || style?.opacity === '0';
  const visibilityFailure = (reason, rect, clipped, style, rejectedAncestor) => ({
    present: true,
    visible: false,
    reachable: false,
    reason,
    rect: numericBounds(rect),
    clipped: numericBounds(clipped),
    ...presentation(style),
    rejectedAncestor: publicElementLabel(rejectedAncestor),
  });
  const publicAlertSummary = () => {
    const alert = document.querySelector('.speaker-runtime[role="alert"], [role="alert"]');
    const text = alert?.textContent?.replace(/\s+/g, ' ').trim();
    if (!text) return null;

    return /(?:[/\\]|embedding|token|api[_-]?key)/i.test(text) ? 'redacted' : text.slice(0, 240);
  };
  const elementVisibility = element => {
    if (!element) return { present: false, visible: false, reachable: false };

    element.scrollIntoView?.({ block: 'center', inline: 'nearest' });
    const rawRect = element.getBoundingClientRect?.();
    if (!rawRect) {
      const empty = { left: 0, right: 0, top: 0, bottom: 0, width: 0, height: 0 };
      return visibilityFailure('zero-size', empty, empty, null, element);
    }

    const rect = bounds(rawRect);
    const style = typeof getComputedStyle === 'function' ? getComputedStyle(element) : null;
    if (isHidden(style)) return visibilityFailure('hidden', rect, rect, style, element);
    if (rect.width <= 0 || rect.height <= 0) return visibilityFailure('zero-size', rect, rect, style, element);

    let clipped = bounds(rect);
    for (let ancestor = element.parentElement; ancestor; ancestor = ancestor.parentElement) {
      const ancestorStyle = typeof getComputedStyle === 'function' ? getComputedStyle(ancestor) : null;
      if (isHidden(ancestorStyle)) {
        return visibilityFailure('hidden', rect, clipped, ancestorStyle, ancestor);
      }

      const clipsChildren = /(?:hidden|clip|scroll|auto)/.test(
        `${ancestorStyle?.overflow} ${ancestorStyle?.overflowX} ${ancestorStyle?.overflowY}`,
      );
      if (clipsChildren && ancestor.getBoundingClientRect) {
        clipped = intersect(clipped, bounds(ancestor.getBoundingClientRect()));
        if (clipped.width <= 0 || clipped.height <= 0) {
          return visibilityFailure('ancestor-clipped', rect, clipped, ancestorStyle, ancestor);
        }
      }
    }
    const viewportWidth = window.innerWidth || document.documentElement?.clientWidth || 0;
    const viewportHeight = window.innerHeight || document.documentElement?.clientHeight || 0;
    clipped = intersect(clipped, { left: 0, top: 0, right: viewportWidth, bottom: viewportHeight });
    if (clipped.width <= 0 || clipped.height <= 0) {
      return visibilityFailure('off-viewport', rect, clipped, style, null);
    }

    const centerX = clipped.left + clipped.width / 2;
    const centerY = clipped.top + clipped.height / 2;
    const hit = document.elementFromPoint?.(centerX, centerY);
    const hitTarget = Boolean(hit) && (hit === element || element.contains?.(hit));
    if (!hitTarget) {
      return {
        present: true,
        visible: true,
        reachable: false,
        reason: 'hit-occluded',
        rect: numericBounds(rect),
        clipped: numericBounds(clipped),
        ...presentation(style),
        rejectedAncestor: null,
      };
    }

    return {
      present: true,
      visible: true,
      reachable: true,
      rect: numericBounds(rect),
      clipped: numericBounds(clipped),
      ...presentation(style),
    };
  };
  const controlVisibility = id => elementVisibility(interactiveControl(id));
  const visibleDialogOpen = () => {
    const dialogs = Array.from(document.querySelectorAll?.('[role="dialog"], [aria-modal="true"], dialog[open]') ?? []);
    try {
      dialogs.push(...document.querySelectorAll?.('dialog:modal') ?? []);
    } catch (_) {
      // 旧 WebKit 不支持 :modal 时仍检查已打开的原生 dialog。
    }
    return dialogs.some(dialog => elementVisibility(dialog).visible);
  };
  const waitForPaint = async () => {
    if (typeof requestAnimationFrame !== 'function') return false;

    const nextFrame = () => new Promise(resolve => {
      let finished = false;
      const finish = painted => {
        if (finished) return;
        finished = true;
        resolve(painted);
      };
      const fallback = setTimeout(() => finish(false), 500);
      requestAnimationFrame(() => {
        clearTimeout(fallback);
        finish(true);
      });
    });
    const firstPainted = await nextFrame();
    const secondPainted = firstPainted && await nextFrame();
    return Boolean(secondPainted);
  };
  const publicRuntime = async () => {
    const diagnostics = {
      navigationPresent: Boolean(input('nav-speakers')),
      enrollControlPresent: Boolean(input('speaker-enroll')),
      profilesControlPresent: Boolean(input('speaker-profiles')),
      pageAlertPresent: Boolean(document.querySelector('.speaker-runtime[role="alert"], [role="alert"]')),
      pageAlert: publicAlertSummary(),
      runtimeStatusPresent: Boolean(document.querySelector('.speaker-runtime[role="status"]')),
      controls: {
        navigation: controlVisibility('nav-speakers'),
        enrollment: controlVisibility('speaker-enroll'),
        profileName: controlVisibility('speaker-profile-name'),
      },
      runtime: null,
      rpcError: null,
    };
    try {
      const diagnosticSignal = typeof AbortSignal?.timeout === 'function'
        ? AbortSignal.timeout(10_000)
        : undefined;
      const response = await fetch('/api/call', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id: 'speaker-runtime-diagnostic', method: 'speaker.runtime', data: {} }),
        signal: diagnosticSignal,
      });
      if (!response.ok) {
        diagnostics.rpcError = `http_${response.status}`;
        return diagnostics;
      }
      const payload = await response.json();
      if (typeof payload.error === 'string') {
        diagnostics.rpcError = /^@yovoice\.[a-zA-Z0-9_.-]+$/.test(payload.error) ? payload.error : 'response_error';
        return diagnostics;
      }
      const result = payload.result;
      if (result && typeof result === 'object') {
        diagnostics.runtime = {
          configured: result.configured === true,
          threshold: typeof result.threshold === 'number' ? result.threshold : null,
          margin: typeof result.margin === 'number' ? result.margin : null,
          timeoutSeconds: typeof result.timeoutSeconds === 'number' ? result.timeoutSeconds : null,
          featureVersion: typeof result.featureVersion === 'string' ? result.featureVersion : null,
          dimension: typeof result.dimension === 'number' ? result.dimension : null,
        };
      }
    } catch (_) {
      diagnostics.rpcError = 'transport';
    }
    return diagnostics;
  };

  try {
    const sampleResponse = await fetch('/speaker-validation-samples.json', {
      signal: AbortSignal.timeout(15_000),
    });
    if (!sampleResponse.ok) throw new Error(`validation sample fixture unavailable: ${sampleResponse.status}`);
    const samples = await sampleResponse.json();
    const startedAt = performance.now();
    const results = [];

  const waitFor = async (predicate, description, timeout = 180_000) => {
    const deadline = performance.now() + timeout;
    while (performance.now() < deadline) {
      const value = predicate();
      if (value) return value;
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    throw new Error(`smoke timeout: ${description}`);
  };

  const textInput = id => {
    const control = input(id);
    const element = control instanceof HTMLInputElement ? control : control?.querySelector('input');
    if (!element) throw new Error(`missing text input: ${id}`);
    return element;
  };
  const click = id => {
    const element = input(id);
    if (!element) throw new Error(`missing control: ${id}`);
    element.click();
  };
  const attach = (target, sample) => {
    const bytes = Uint8Array.from(atob(sample.base64), character => character.charCodeAt(0));
    const files = new DataTransfer();
    files.items.add(new File([bytes], sample.name, { type: 'audio/wav' }));
    const element = input(target);
    if (!element) throw new Error(`missing file input: ${target}`);
    element.files = files.files;
    element.dispatchEvent(new Event('change', { bubbles: true }));
  };
  const profileRows = () => Array.from(document.querySelectorAll('[data-speaker-profile-row="true"]'));
  const profileIDs = () => profileRows().map(element => {
    const profileID = element.dataset.profileId;
    if (!profileID) throw new Error('speaker profile row is missing its public ID');
    return profileID;
  });
  const profilesLoaded = () => input('speaker-profiles')?.dataset.state === 'ready';

  const navigation = await waitFor(() => {
    const visibility = controlVisibility('nav-speakers');
    return visibility.reachable ? interactiveControl('nav-speakers') : null;
  }, 'visible speaker navigation', 30_000);
  navigation.click();
  const runtime = await publicRuntime();
  if (runtime.rpcError) throw new Error(`speaker runtime diagnostic failed: ${runtime.rpcError}`);
  if (runtime.runtime?.configured !== true) throw new Error('speaker runtime is not configured');

  const readyControls = await waitFor(() => {
    const enrollment = controlVisibility('speaker-enroll');
    const profileName = controlVisibility('speaker-profile-name');
    const runtimeStatus = Boolean(document.querySelector('.speaker-runtime[role="status"]'));
    return runtimeStatus && enrollment.reachable && profileName.reachable
      ? { enrollment, profileName }
      : null;
  }, 'visible configured speaker enrollment controls', 30_000);

  const register = async (sample, name) => {
    attach('speaker-enroll-file', sample);
    const nameInput = textInput('speaker-profile-name');
    const valueSetter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;
    valueSetter.call(nameInput, name);
    nameInput.dispatchEvent(new Event('input', { bubbles: true }));
    await waitFor(() => input('speaker-enroll') && !input('speaker-enroll').disabled, `register input ${name}`);
    const before = profileIDs();
    const operationStartedAt = performance.now();
    click('speaker-enroll');
    const row = await waitFor(() => profileRows().find(element => {
      const profileID = element.dataset.profileId;
      return profileID && !before.includes(profileID) ? element : undefined;
    }), `register ${name}`);
    const id = row.dataset.profileId;
    if (!id) throw new Error(`registered ${name} without a public profile ID`);
    await waitFor(profilesLoaded, `profile list after registering ${name}`);
    results.push({ operation: 'register', name, id, expected: 'created', actual: 'created', durationMs: Math.round(performance.now() - operationStartedAt) });
    return id;
  };

  const identify = async (sample, expected, expectedID) => {
    attach('speaker-identify-file', sample);
    const operationStartedAt = performance.now();
    click('speaker-identify');
    const result = await waitFor(() => document.querySelector('[data-testid="speaker-result"]'), `identify ${sample.name}`);
    const status = result.dataset.status;
    const profileID = result.dataset.profileId || null;
    const score = result.dataset.score === '' ? null : Number(result.dataset.score);
    const threshold = Number(result.dataset.threshold);
    const margin = Number(result.dataset.margin);
    const candidateCount = Number(result.dataset.candidateCount);
    const candidateIDs = Array.from(result.querySelectorAll('[data-speaker-candidate-id]')).map(element => {
      const candidateID = element.dataset.speakerCandidateId;
      if (!candidateID) throw new Error(`identify result has a candidate without a public profile ID for ${sample.name}`);
      return candidateID;
    });
    const unexpectedKnownProfile = expectedID && profileID !== expectedID;
    const unknownWithProfile = expected === 'unknown' && profileID !== null;
    const expectedCandidateMissing = expectedID && !candidateIDs.includes(expectedID);
    const candidatesDoNotMatchResult = candidateIDs.length !== candidateCount;
    if (status !== expected || unexpectedKnownProfile || unknownWithProfile || expectedCandidateMissing || candidatesDoNotMatchResult) {
      throw new Error(`unexpected identify result for ${sample.name}: ${status}`);
    }
    if (!Number.isFinite(threshold) || !Number.isFinite(margin) || !Number.isFinite(candidateCount)) {
      throw new Error(`identify result did not expose public decision parameters for ${sample.name}`);
    }
    results.push({
      operation: 'identify',
      sample: sample.name,
      expected,
      actual: status,
      profileID,
      score,
      threshold,
      margin,
      candidateCount,
      candidateIDs,
      profileCount: profileIDs().length,
      durationMs: Math.round(performance.now() - operationStartedAt),
      uiTerminal: 'speaker-result',
    });
  };

  const aID = await register(samples['speaker-a-enroll'], 'Speaker A');
  const bID = await register(samples['speaker-b-enroll'], 'Speaker B');
  await identify(samples['speaker-a-query'], 'matched', aID);
  await identify(samples['speaker-b-query'], 'matched', bID);
  await identify(samples['unknown-query'], 'unknown');

  click(`speaker-delete-${bID}`);
  const confirmation = await waitFor(() => input('speaker-delete-confirm'), 'delete confirmation');
  confirmation.click();
  const deleteStartedAt = performance.now();
  await waitFor(() => profilesLoaded() && !profileIDs().includes(bID) && profileIDs().length === 1, 'deleted profile');
  results.push({ operation: 'delete', id: bID, expected: 'deleted', actual: 'deleted', durationMs: Math.round(performance.now() - deleteStartedAt) });

  const remainingProfile = await waitFor(() => {
    if (visibleDialogOpen()) return null;
    const row = profileRows().find(element => Boolean(element.dataset.profileId));
    return row && elementVisibility(row).reachable ? row : null;
  }, 'visible remaining profile after deletion');
  const painted = await waitForPaint();
  const remainingProfileVisibility = elementVisibility(remainingProfile);
  if (!painted || visibleDialogOpen() || !remainingProfileVisibility.reachable) {
    throw new Error('speaker page did not reach a visible stable state after deletion');
  }

  const pageText = document.body.innerText;
  if (pageText.includes('/artifacts/') || pageText.includes('embedding')) throw new Error('speaker page exposed private runtime details');
    return {
      profileIDs: profileIDs(),
      results,
      elapsedMs: Math.round(performance.now() - startedAt),
      uiVisibility: {
        ...readyControls,
        remainingProfile: remainingProfileVisibility,
      },
    };
  } catch (error) {
    const detail = error instanceof Error ? `${error.message}\n${error.stack ?? ''}` : String(error);
    return { failure: detail, diagnostics: await publicRuntime() };
  }
})()
