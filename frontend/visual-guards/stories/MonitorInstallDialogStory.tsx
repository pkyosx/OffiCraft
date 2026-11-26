// Story — the real 安裝機器 dialog (MachineInstallDialog) for a remote machine,
// on the mock api seam, so the boot command is the mock's fake one-liner.
import { useEffect, useLayoutEffect } from "react";
import { I18nProvider, useI18n } from "../../src/i18n";
import { MachineInstallDialog } from "../../src/components/MonitorPage";
import { api } from "../../src/api";
import "../../src/components/monitor.css";

function LanguageSwitch({ language }: { language: "zh" | "en" }) {
  const { language: current, setLanguage } = useI18n();
  useEffect(() => {
    if (language !== current) setLanguage(language);
  }, [language, current, setLanguage]);
  return null;
}

export function MonitorInstallDialogStory({
  language = "zh",
  fetchFails = false,
}: {
  language?: "zh" | "en";
  /** The boot command cannot be fetched (the dialog's 取得指令失敗 state). */
  fetchFails?: boolean;
}) {
  // A layout effect runs before the dialog's own fetch effect, so the dialog
  // sees the failing fetch; the original is put back on unmount.
  useLayoutEffect(() => {
    if (!fetchFails) return;
    const original = api.getMachineBootCommand;
    api.getMachineBootCommand = () => Promise.reject(new Error("offline"));
    return () => {
      api.getMachineBootCommand = original;
    };
  }, [fetchFails]);
  return (
    <I18nProvider>
      <LanguageSwitch language={language} />
      <MachineInstallDialog
        machineId="m-alpha"
        displayName="Alpha"
        onClose={() => {}}
      />
    </I18nProvider>
  );
}
