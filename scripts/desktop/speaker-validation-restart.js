return (async () => {
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
  const controlVisibility = element => {
    if (!element?.isConnected) return { present: false, visible: false, reachable: false };

    const rawRect = element.getBoundingClientRect?.();
    if (!rawRect) {
      const empty = { left: 0, right: 0, top: 0, bottom: 0, width: 0, height: 0 };
      return visibilityFailure('zero-size', empty, empty, null, element);
    }
    const rect = bounds(rawRect);
    const elementStyle = getComputedStyle(element);
    if (isHidden(elementStyle)) return visibilityFailure('hidden', rect, rect, elementStyle, element);
    if (rect.width <= 0 || rect.height <= 0) return visibilityFailure('zero-size', rect, rect, elementStyle, element);

    let clipped = bounds(rect);
    for (let ancestor = element.parentElement; ancestor; ancestor = ancestor.parentElement) {
      const style = getComputedStyle(ancestor);
      if (isHidden(style)) return visibilityFailure('hidden', rect, clipped, style, ancestor);

      const clipsChildren = /(?:hidden|clip|scroll|auto)/.test(
        `${style.overflow} ${style.overflowX} ${style.overflowY}`,
      );
      if (clipsChildren) {
        clipped = intersect(clipped, bounds(ancestor.getBoundingClientRect()));
        if (clipped.width <= 0 || clipped.height <= 0) {
          return visibilityFailure('ancestor-clipped', rect, clipped, style, ancestor);
        }
      }
    }

    const viewport = {
      left: 0,
      top: 0,
      right: window.innerWidth || document.documentElement.clientWidth,
      bottom: window.innerHeight || document.documentElement.clientHeight,
    };
    clipped = intersect(clipped, viewport);
    if (clipped.width <= 0 || clipped.height <= 0) {
      return visibilityFailure('off-viewport', rect, clipped, elementStyle, null);
    }

    const centerX = clipped.left + clipped.width / 2;
    const centerY = clipped.top + clipped.height / 2;
    const hit = document.elementFromPoint?.(centerX, centerY);
    const hitTarget = Boolean(hit) && (hit === element || element.contains(hit));
    if (!hitTarget) {
      return {
        present: true,
        visible: true,
        reachable: false,
        reason: 'hit-occluded',
        rect: numericBounds(rect),
        clipped: numericBounds(clipped),
        ...presentation(elementStyle),
        rejectedAncestor: null,
      };
    }
    return {
      present: true,
      visible: true,
      reachable: hitTarget,
      rect: numericBounds(rect),
      clipped: numericBounds(clipped),
      ...presentation(elementStyle),
    };
  };
  const renderedDialogOpen = () => {
    const dialogs = Array.from(document.querySelectorAll?.('[role="dialog"], [aria-modal="true"], dialog[open]') ?? []);
    try {
      dialogs.push(...document.querySelectorAll?.('dialog:modal') ?? []);
    } catch (_) {
      // 旧 WebKit 不支持 :modal 时仍检查已打开的原生 dialog。
    }
    return dialogs.some(dialog => controlVisibility(dialog).visible);
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
    const navigation = document.querySelector('[data-testid="nav-speakers"]');
    const profileList = document.querySelector('[data-testid="speaker-profiles"]');
    const runtime = document.querySelector('.speaker-runtime');
    return {
      navigation: controlVisibility(navigation),
      profiles: controlVisibility(profileList),
      pageAlertPresent: Boolean(document.querySelector('.speaker-runtime[role="alert"], [role="alert"]')),
      runtimeStatusPresent: Boolean(document.querySelector('.speaker-runtime[role="status"]')),
      runtimeConfigured: runtime?.getAttribute('role') === 'status',
      visibleDialogOpen: renderedDialogOpen(),
    };
  };

  try {
    const waitFor = async (predicate, description, timeout = 30_000) => {
      const deadline = performance.now() + timeout;
      while (performance.now() < deadline) {
        const value = predicate();
        if (value) return value;
        await new Promise(resolve => setTimeout(resolve, 100));
      }
      throw new Error(`restart smoke timeout: ${description}`);
    };
    const navigation = await waitFor(() => {
      const element = document.querySelector('[data-testid="nav-speakers"]');
      return controlVisibility(element).reachable ? element : null;
    }, 'visible speaker navigation');
    navigation.click();

    await waitFor(
      () => navigation.getAttribute?.('aria-current') === 'page',
      'speaker navigation selection',
    );
    await waitFor(
      () => controlVisibility(document.querySelector('.speaker-runtime[role="status"]')).visible,
      'configured speaker runtime',
    );
    await waitFor(() => {
      const list = document.querySelector('[data-testid="speaker-profiles"]');
      return list?.dataset.state === 'ready';
    }, 'loaded profile list');
    const selectedProfile = await waitFor(() => {
      if (renderedDialogOpen()) return null;
      const row = Array.from(document.querySelectorAll('[data-speaker-profile-row="true"]'))
        .find(element => Boolean(element.dataset.profileId));
      if (!row) return null;
      row.scrollIntoView?.({ block: 'center', inline: 'nearest' });
      return controlVisibility(row).reachable ? row : null;
    }, 'visible persisted speaker profile');
    const painted = await waitForPaint();

    const profileVisibility = controlVisibility(selectedProfile);
    if (!painted || renderedDialogOpen() || !profileVisibility.reachable) {
      throw new Error('speaker restart did not reach a visible stable profile state');
    }

    return {
      profileIDs: Array.from(document.querySelectorAll('[data-speaker-profile-row="true"]')).map(element => {
        const profileID = element.dataset.profileId;
        if (!profileID) throw new Error('speaker profile row is missing its public ID after restart');
        return profileID;
      }),
      readiness: {
        navigation: controlVisibility(navigation),
        profile: profileVisibility,
      },
    };
  } catch (error) {
    const detail = error instanceof Error ? `${error.message}\n${error.stack ?? ''}` : String(error);
    return { failure: detail, diagnostics: await publicRuntime() };
  }
})()
