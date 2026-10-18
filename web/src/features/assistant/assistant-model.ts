import type { AssistantBlock, AssistantMessage, AssistantToolCard } from '../../shared/workbench';

// 助手一轮内容的展示模型：参照 astrolabe 的会话流，把按顺序的内容块分成“过程”（思考、工具、中间说明）
// 与“回答”（最后一次工具调用之后的文字），运行中再给出此刻在做什么。

/** 消息的内容块：新记录直接用 blocks；旧记录只有 text 与 tools，拼成工具在前、文本在后。 */
export function messageBlocks(message: AssistantMessage): AssistantBlock[] {
  if (message.blocks?.length) return message.blocks;
  const tools = (message.tools ?? []).map(card => ({ kind: 'tool' as const, card }));
  return message.text ? [...tools, { kind: 'text' as const, text: message.text }] : tools;
}

/** 把一轮分成过程与回答：最后一次工具调用之后的文本是回答，其余非空块归入过程。 */
export function splitTurn(blocks: AssistantBlock[]): { process: AssistantBlock[]; answer: string } {
  const lastTool = blocks.map(block => block.kind).lastIndexOf('tool');
  const process: AssistantBlock[] = [];
  const answer: string[] = [];
  blocks.forEach((block, index) => {
    if (block.kind === 'text' && index > lastTool) {
      if (block.text?.trim()) answer.push(block.text);
    } else if (block.kind === 'tool' || block.text?.trim()) {
      process.push(block);
    }
  });
  return { process, answer: answer.join('\n\n') };
}

export type TurnActivity = { kind: 'thinking' } | { kind: 'tool'; card: AssistantToolCard } | { kind: 'writing' };

/** 运行中的一轮此刻在做什么：有运行中的工具时是该工具，最后一块是文本时在输出，其余时候在思考。 */
export function turnActivity(blocks: AssistantBlock[]): TurnActivity {
  const running = [...blocks].reverse().find(block => block.card?.state === 'running');
  if (running?.card) return { kind: 'tool', card: running.card };
  return blocks.at(-1)?.kind === 'text' ? { kind: 'writing' } : { kind: 'thinking' };
}

/** 一轮里的工具卡片，按出现顺序。 */
export const turnCards = (blocks: AssistantBlock[]) => blocks.flatMap(block => block.card ? [block.card] : []);
