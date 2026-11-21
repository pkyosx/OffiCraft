// Story — the real 安裝機器 dialog (MachineInstallDialog) for a remote machine,
// on the mock api seam, so the boot command is the mock's fake one-liner.
import { useEffect } from "react";
import { I18nProvider, useI18n } from "../../src/i18n";
import { MachineInstallDialog } from "../../src/components/MonitorPage";
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
}: {
  language?: "zh" | "en";
}) {
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
