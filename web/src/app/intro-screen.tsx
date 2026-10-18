import { useEffect, useRef, useState } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { Token } from '@astryxdesign/core/Token';
import { useTranslator } from '@astryxdesign/core/i18n';
import { FileMusic, Mic, Music, Pencil } from 'lucide-react';

const kinds = [['text', Mic], ['story', Pencil], ['music', Music], ['score', FileMusic]] as const;
// 按钮在约 4.4 秒时出现，与 styles.css 中 .intro-start 的动画延迟一致。
const buttonDelayMs = 4400;
const leaveMs = 400;

/**
 * 首次启动的开场画面：接续 index.html 的启动动画，约 5 秒依次展开标语、四种创作方式，
 * 最后出现「开始探索」，点击后淡出进入主界面。减少动态效果时全部内容直接显示。
 */
export function IntroScreen({ onStart }: { onStart: () => void }) {
  const t = useTranslator();
  const root = useRef<HTMLElement>(null);
  const [leaving, setLeaving] = useState(false);
  useEffect(() => {
    // 按钮出现后把焦点交给它，键盘用户直接按回车即可进入。
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    const timer = window.setTimeout(() => root.current?.querySelector<HTMLButtonElement>('.intro-start button, button.intro-start')?.focus(), reduced ? 0 : buttonDelayMs);
    return () => window.clearTimeout(timer);
  }, []);
  const start = () => {
    if (leaving) return;
    setLeaving(true);
    window.setTimeout(onStart, leaveMs);
  };
  // 图标与名字停在启动动画的同一位置，其余内容从下方依次展开，切换时画面不跳动。
  return <VStack ref={root} as="section" className="intro-screen" data-leaving={leaving || undefined} role="dialog" aria-modal="true" aria-label="yovoice" hAlign="center" vAlign="center" gap={0}>
    <VStack className="intro-head" hAlign="center" gap={5}>
      <span className="intro-tile" aria-hidden><i /><i /><i /><i /><i /></span>
      <strong className="intro-name">yovoice</strong>
    </VStack>
    <VStack className="intro-body" hAlign="center" gap={5}>
      <p className="intro-tagline">{t('@yovoice.intro.tagline')}</p>
      <HStack className="intro-kinds" gap={2} hAlign="center" wrap="wrap">
        {kinds.map(([kind, Icon]) => <Token key={kind} color="teal" size="lg" icon={<Icon size={14} />} label={t(`@yovoice.project.${kind}`)} />)}
      </HStack>
      <Button className="intro-start" variant="primary" size="lg" label={t('@yovoice.intro.start')} onClick={start} />
    </VStack>
  </VStack>;
}
