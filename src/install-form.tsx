import { useState } from "react";

import { Confirmation } from "./confirmation";
import { Selection } from "./selection";
import type { Theme } from "./theme";

// ── install form ─────────────────────────────────────────────────────────────
//
// The first-run screen (story 42-43): pick a date format (mono-select), then
// confirm nerdfont support (y/n, default no). It draws the form and returns the
// values; persisting them to config.md is Spec 0003. It runs in no-nerdfont mode
// because that is exactly the setting the user has not chosen yet.

export interface InstallResult {
  dateFormat: "america" | "europe";
  nerdfont: boolean;
}

export interface InstallFormProps {
  theme: Theme;
  onComplete: (result: InstallResult) => void;
}

const DATE_FORMATS = [
  { label: "american (yyyy/mm/dd)", value: "america" },
  { label: "europe (dd/mm/yyyy)", value: "europe" },
];

export function InstallForm({ theme, onComplete }: InstallFormProps) {
  const [dateFormat, setDateFormat] = useState<"america" | "europe" | null>(null);

  if (dateFormat === null) {
    return (
      <Selection
        title="Date format"
        items={DATE_FORMATS}
        nerdfont={false}
        theme={theme}
        onSubmit={([value]) => setDateFormat(value as "america" | "europe")}
      />
    );
  }

  return (
    <Confirmation
      title="Nerdfont"
      question="do you support nerdfonts?"
      nerdfont={false}
      theme={theme}
      onSubmit={(nerdfont) => onComplete({ dateFormat, nerdfont })}
    />
  );
}
