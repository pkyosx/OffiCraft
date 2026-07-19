import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { I18nProvider } from "./i18n";
import { AuthGate } from "./AuthGate";
import "./styles/theme.css";
import "./styles/global.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <I18nProvider>
      <AuthGate />
    </I18nProvider>
  </StrictMode>
);
