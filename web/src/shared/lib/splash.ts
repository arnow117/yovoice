/** 移除 index.html 中的启动动画：先淡出，淡出结束后删除节点；可重复调用。 */
export function dismissSplash() {
  const splash = document.getElementById('splash');
  if (!splash || splash.dataset.leaving) return;
  splash.dataset.leaving = 'true';
  const remove = () => splash.remove();
  splash.addEventListener('transitionend', remove, { once: true });
  // 减少动态效果或页面在后台时可能收不到 transitionend，到时直接删除。
  window.setTimeout(remove, 600);
}
