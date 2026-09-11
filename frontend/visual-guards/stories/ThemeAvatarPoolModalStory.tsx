import { useState } from "react";
import { ThemeAvatarPoolModal } from "../../src/components/ThemeAvatarPoolModal";
import "../../src/components/theme-settings.css";

const PIXEL =
  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Z2S8AAAAASUVORK5CYII=";

export function ThemeAvatarPoolModalStory() {
  const [pool, setPool] = useState(() =>
    Array.from({ length: 7 }, (_, index) => ({
      id: `icn-slot-${index}`,
      image: `${PIXEL}#slot-${index}`,
    })),
  );

  return (
    <ThemeAvatarPoolModal
      kind="member"
      title="正職頭像"
      hint="可新增、替換或移除圖片。這個版本不支援調整順序。"
      pool={pool}
      addLabel="新增圖片"
      replaceLabel="替換圖片"
      removeLabel="移除圖片"
      clearLabel="清除"
      closeLabel="關閉"
      doneLabel="完成"
      emptyLabel="尚未新增圖片"
      onAdd={() => {}}
      onReplace={() => {}}
      onRemove={(index) =>
        setPool((current) => current.filter((_, itemIndex) => itemIndex !== index))
      }
      onClear={() => setPool([])}
      onClose={() => {}}
    />
  );
}
