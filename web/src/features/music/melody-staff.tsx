import { useEffect, useRef } from 'react';
import { VStack } from '@astryxdesign/core/Layout';
import type { NoteTimingEvent } from 'abcjs';

// 五线谱视图：用 abcjs 按 ABC 谱面排版，人声与伴奏两行成组、和弦标在人声上方；abcjs 按需加载，不进首屏包。
// 试听时按播放时间高亮正在响的音符，并在谱面框内滚动跟随；点音符跳到对应时间。
export function MelodyStaff({ abc, label, time, active, seek }: {
  abc: string; label: string; time: number; active: boolean; seek: (seconds: number) => void;
}) {
  const box = useRef<HTMLDivElement>(null);
  const host = useRef<HTMLDivElement>(null);
  const timings = useRef<NoteTimingEvent[]>([]);
  const lit = useRef<Element[]>([]);
  const seekRef = useRef(seek);
  useEffect(() => { seekRef.current = seek; });

  useEffect(() => {
    let live = true;
    // 编辑谱面时稍等再排版，避免每个字符都重排整页。
    const timer = setTimeout(() => {
      void import('abcjs').then(({ default: abcjs }) => {
        if (!live || !host.current) return;
        const [tune] = abcjs.renderAbc(host.current, abc, {
          responsive: 'resize', add_classes: true, paddingtop: 0, paddingbottom: 0, paddingleft: 0, paddingright: 0, foregroundColor: 'currentColor',
          clickListener: element => {
            // 按谱面字符位置找到这个音符在时间表里的第一次出现。
            const event = timings.current.find(item => item.startChar === element.startChar);
            if (event) seekRef.current(event.milliseconds / 1000);
          },
        });
        lit.current = [];
        // 只取音符时间表，不启动 abcjs 自己的计时器，进度以试听音频为准。
        timings.current = tune ? new abcjs.TimingCallbacks(tune, {}).noteTimings.filter(event => event.type === 'event' && event.elements?.length) : [];
      });
    }, 150);
    return () => { live = false; clearTimeout(timer); };
  }, [abc]);

  useEffect(() => {
    lit.current.forEach(element => element.classList.remove('melody-note-playing'));
    lit.current = [];
    if (!active) return;
    const ms = time * 1000;
    const events = timings.current;
    // 二分找到最后一个已开始的音符事件。
    let low = 0, high = events.length - 1, found = -1;
    while (low <= high) {
      const mid = (low + high) >> 1;
      if (events[mid].milliseconds <= ms) { found = mid; low = mid + 1; } else high = mid - 1;
    }
    if (found < 0) return;
    const elements = (events[found].elements ?? []).flat();
    elements.forEach(element => element.classList.add('melody-note-playing'));
    lit.current = elements;
    // 只在谱面框内滚动，不带动整页。
    const frame = box.current, first = elements[0];
    if (!frame || !first) return;
    const top = first.getBoundingClientRect().top - frame.getBoundingClientRect().top;
    if (top < 0 || top > frame.clientHeight * 0.75) frame.scrollTo({ top: frame.scrollTop + top - frame.clientHeight / 3, behavior: 'smooth' });
  }, [time, active]);

  return <VStack ref={box} className="melody-staff" gap={0} role="img" aria-label={label}><div ref={host} /></VStack>;
}
