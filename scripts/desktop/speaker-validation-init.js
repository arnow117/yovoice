return (async () => {
  const deadline = performance.now() + 30_000;
  while (performance.now() < deadline) {
    const navigation = document.querySelector('[data-testid="nav-speakers"]');
    if (navigation) {
      navigation.click();
      if (document.querySelector('[data-testid="speaker-enroll"]')) return { initialized: true };
    }
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  throw new Error('validation initialization timeout');
})()
