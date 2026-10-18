import { test, expect, type Page } from '@playwright/test';

// 助手面板：打开与快捷键、发送与流式回复、工具卡片联动编辑器、停止、新对话、运行时缺失。
const shots = '/private/tmp/claude-501/-Users-leemysw-Projects-leemysw-yovoice/1d41db9d-becd-47ff-a400-e5ce20d3aca3/scratchpad/shots';

async function configureAI(page: Page) {
  // 浮动面板会盖住设置页的按钮，配置时先收起。
  if (await page.getByTestId('assistant-panel').isVisible().catch(() => false)) await page.keyboard.press('ControlOrMeta+j');
  await page.getByTestId('nav-settings').click();
  await page.getByRole('tab', { name: 'AI 服务' }).click();
  await page.getByRole('button', { name: 'Ollama', exact: true }).click();
  await page.getByRole('textbox', { name: '当前模型' }).fill('qwen3');
  await page.getByRole('button', { name: '添加服务', exact: true }).click();
  await expect(page.locator('.ai-nav').getByLabel('使用中')).toHaveCount(1);
}

test('助手面板：⌘J 开关、空状态引导与快捷胶囊，状态跨刷新记住', async ({ page }) => {
  await page.goto('/');
  await configureAI(page);
  // 默认收起，不遮挡工作区；⌘J 打开。
  const panel = page.getByTestId('assistant-panel');
  await expect(panel).toHaveCount(0);
  await page.keyboard.press('ControlOrMeta+j');
  await expect(panel).toBeVisible();
  await expect(panel.getByText('用一句话让助手动手')).toBeVisible();
  // 语音页的快捷胶囊：空状态展示。
  await expect(panel.getByRole('button', { name: '帮我挑个音色', exact: true })).toBeVisible();
  await panel.screenshot({ path: `${shots}/assistant-empty.png` });
  await page.keyboard.press('ControlOrMeta+j');
  await expect(panel).toHaveCount(0);
  // 开关状态记住：打开后刷新仍打开，收起后刷新仍收起。
  await page.keyboard.press('ControlOrMeta+j');
  await expect(panel).toBeVisible();
  await page.reload();
  await page.getByTestId('assistant-toggle').waitFor({ state: 'visible' });
  await expect(page.getByTestId('assistant-panel')).toBeVisible();
  await page.keyboard.press('ControlOrMeta+j');
  await page.reload();
  await page.getByTestId('assistant-toggle').waitFor({ state: 'visible' });
  await expect(page.getByTestId('assistant-panel')).toHaveCount(0);
});

test('助手对话：发送后流式回复，工具卡片联动编辑器与页面，可停止、可新对话', async ({ page }) => {
  await page.goto('/');
  await configureAI(page);
  await page.keyboard.press('ControlOrMeta+j');
  const panel = page.getByTestId('assistant-panel');
  const input = panel.getByRole('textbox');
  await input.fill('帮我写四小节钢琴旋律');
  await input.press('Enter');
  // 运行中：发送变为停止，流式文本逐步出现。
  await expect(panel.getByRole('button', { name: /停止|Stop/ })).toBeVisible();
  await expect(panel.locator('.assistant-reply')).toContainText(/写好了|钢琴/);
  // 工具卡片：新建作品并写入乐谱。
  await expect(panel.getByText('新建了《示例小步舞曲》').first()).toBeVisible();
  await expect(panel.getByText(/修改了《示例小步舞曲》的乐谱/).first()).toBeVisible();
  await panel.screenshot({ path: `${shots}/assistant-chat.png` });
  // 界面被助手接管：跳到新作品，编辑器里出现乐谱。
  await expect(page.getByLabel('作品名称')).toHaveValue('示例小步舞曲');
  await expect(page.locator('.score-summary')).toContainText('小节');
  // 本轮结束后回到输入态；追问一条，随即停止。
  await expect(panel.getByRole('textbox')).toBeVisible();
  await panel.getByRole('textbox').fill('再快一点');
  await panel.getByRole('textbox').press('Enter');
  await panel.getByRole('button', { name: /停止|Stop/ }).click();
  await expect(panel.getByText('已停止')).toBeVisible();
  await expect(panel.getByRole('textbox')).toBeVisible();
  // 助手回复可复制。
  await panel.getByRole('button', { name: '复制', exact: true }).first().click();
  await expect(panel.getByRole('button', { name: '已复制', exact: true }).first()).toBeVisible();
  // 上下文标签显示当前作品，可点掉不附带。
  await expect(panel.locator('.assistant-context')).toContainText('示例小步舞曲');
  await panel.locator('.assistant-context').click();
  await expect(panel.locator('.assistant-context')).toHaveClass(/off/);
  await panel.locator('.assistant-context').click();
  // 新会话：面板清空，标题回默认。
  await panel.getByRole('button', { name: '新对话', exact: true }).click();
  await expect(panel.getByText('用一句话让助手动手')).toBeVisible();
  await panel.getByRole('textbox').fill('你好');
  await panel.getByRole('textbox').press('Enter');
  await expect(panel.getByText('你好', { exact: true }).first()).toBeVisible();
  await expect(panel.getByText('好的，我先看了作品列表。').first()).toBeVisible();
  // 历史抽屉：两个会话按更新时间列出，可切换回旧会话、可删除。
  await panel.getByTestId('assistant-history-toggle').click();
  const history = page.getByTestId('assistant-history');
  await expect(history).toContainText('2 个会话');
  await expect(history).toContainText('帮我写四小节钢琴旋律');
  await panel.screenshot({ path: `${shots}/assistant-history.png` });
  await history.getByRole('button', { name: '帮我写四小节钢琴旋律', exact: true }).click();
  await expect(panel.getByText('新建了《示例小步舞曲》').first()).toBeVisible();
  await panel.getByTestId('assistant-history-toggle').click();
  await history.getByRole('button', { name: /删除会话：你好/ }).click();
  await history.getByRole('button', { name: '删除', exact: true }).click();
  await expect(history).toContainText('1 个会话');
  await history.getByRole('button', { name: '返回对话', exact: true }).click();
  await expect(history).toHaveCount(0);
  // 展开/收起为右侧全高面板。
  await panel.getByRole('button', { name: '展开面板', exact: true }).click();
  await expect(panel).toHaveClass(/expanded/);
  await panel.screenshot({ path: `${shots}/assistant-expanded.png` });
  await page.screenshot({ path: `${shots}/assistant-expanded-page.png` });
  // 深色系统偏好下的外观（应用为浅色主题，仅验证不破版）。
  await page.emulateMedia({ colorScheme: 'dark' });
  await panel.screenshot({ path: `${shots}/assistant-dark.png` });
});

test('编辑器 AI 写作入口：提交即进助手对话，面板自动打开', async ({ page }) => {
  await page.goto('/');
  await configureAI(page);
  await page.keyboard.press('ControlOrMeta+j');
  await page.getByTestId('assistant-panel').getByRole('button', { name: '关闭', exact: true }).click();
  await page.getByTestId('nav-new').click();
  await page.getByRole('menuitem', { name: '编曲', exact: true }).click();
  const composer = page.locator('.ai-composer');
  await expect(composer).toContainText('Ollama · qwen3');
  await composer.getByRole('textbox', { name: '描述想要的配乐' }).fill('温暖舒缓的钢琴');
  await composer.getByRole('button', { name: '写谱', exact: true }).click();
  // 需求作为一条消息进入面板，输入框清空。
  const panel = page.getByTestId('assistant-panel');
  await expect(panel).toBeVisible();
  await expect(panel.getByText('给当前编曲作品写谱：温暖舒缓的钢琴', { exact: false }).last()).toBeVisible();
  await expect(composer.getByRole('textbox', { name: '描述想要的配乐' })).toHaveValue('');
});

test('未配置 AI 服务：面板引导去设置，配置后即可用', async ({ page }) => {
  await page.goto('/');
  await page.keyboard.press('ControlOrMeta+j');
  const panel = page.getByTestId('assistant-panel');
  await expect(panel).toContainText('还没有可用的 AI 服务');
  await panel.getByRole('button', { name: '设置 AI', exact: true }).click();
  await expect(page.getByRole('tab', { name: 'AI 服务', selected: true })).toBeVisible();
  await configureAI(page);
  await page.keyboard.press('ControlOrMeta+j');
  await expect(page.getByTestId('assistant-panel').getByRole('textbox')).toBeVisible();
});
