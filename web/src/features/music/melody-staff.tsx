import { useEffect, useRef } from 'react';
import { VStack } from '@astryxdesign/core/Layout';

// 五线谱视图：用 abcjs 按 ABC 谱面排版，人声与伴奏两行成组、和弦标在人声上方；abcjs 按需加载，不进首屏包。
export function MelodyStaff({ abc, label }: { abc: string; label: string }) {
  const host = useRef<HTMLDivElement>(null);
  useEffect(() => {
    let live = true;
    // 编辑谱面时稍等再排版，避免每个字符都重排整页。
    const timer = setTimeout(() => {
      void import('abcjs').then(({ default: abcjs }) => {
        if (!live || !host.current) return;
        abcjs.renderAbc(host.current, abc, { responsive: 'resize', add_classes: true, paddingtop: 0, paddingbottom: 0, paddingleft: 0, paddingright: 0, foregroundColor: 'currentColor' });
      });
    }, 150);
    return () => { live = false; clearTimeout(timer); };
  }, [abc]);
  return <VStack className="melody-staff" gap={0} role="img" aria-label={label}><div ref={host} /></VStack>;
}
