import { useEffect, useState } from 'react';

// 临时试听只保存页面内 Blob；替换或卸载时回收 Object URL，不进入声音库。
export function useTemporaryAudio(blob: Blob | null) {
  const [url, setURL] = useState('');

  useEffect(() => {
    if (!blob) {
      setURL('');
      return;
    }
    const next = URL.createObjectURL(blob);
    setURL(next);
    return () => URL.revokeObjectURL(next);
  }, [blob]);

  return url;
}
