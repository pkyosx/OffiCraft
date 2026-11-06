import { I18nProvider } from "../../src/i18n";
import { ReplyCardWaitingBody } from "../../src/components/ReplyCardBody";
import { SecretValueInput } from "../../src/components/SecretValueInput";
import "../../src/components/secrets.css";
import type { ReplyCard } from "../../src/api/adapter";

const SECRET_CARD: ReplyCard = {
  id: "rc-secret",
  from: "mira",
  kind: "secret",
  summary: "部署金鑰",
  body: "",
  options: [],
  selectMode: "single",
  status: "waiting",
  attachments: [],
  createdTs: Date.now() / 1000 - 600,
  answeredTs: null,
  chatMessageId: "msg-secret",
  answer: null,
  secret: { state: "waiting", providedTs: null, retrievedTs: null, expiresTs: null },
};

export function ReplySecretStory() {
  return (
    <I18nProvider>
      <article className="reply-card" data-testid="card-secret">
        <ReplyCardWaitingBody
          card={SECRET_CARD}
          onAnswer={() => Promise.resolve()}
          onProvideSecret={() => Promise.resolve()}
        />
      </article>
    </I18nProvider>
  );
}

export function SecretsValueStory({ value }: { value: string }) {
  return (
    <I18nProvider>
      <label className="secrets__field">
        <SecretValueInput
          className="secrets__input secrets__textarea secrets__value"
          data-testid="secrets-value"
          value={value}
          onChange={() => {}}
        />
      </label>
    </I18nProvider>
  );
}
