import { useEffect, useLayoutEffect, useState } from "react";
import { Box, Text } from "ink";

import { createGlyphs } from "./glyph";
import { inkColor } from "./ink-color";
import type { Theme } from "./theme";

// ── download progress ────────────────────────────────────────────────────────
//
// update apply's two-line status: a braille spinner beside the fixed pending
// message ("updating taskthing..."), and beneath it a sub-line that tracks
// which part of apply is running — the binary download (a live bar) or the
// swap + migrations that follow it ("applying update..."). It settles into the
// same single success/failure line Spinner draws, both lines gone at once.

const FRAMES = ["⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"];
const FRAME_MS = 80;
const BAR_WIDTH = 24;

type Status =
  | { kind: "running" }
  | { kind: "success"; message: string }
  | { kind: "failure"; message: string };

export type DownloadPhase =
  | { kind: "downloading"; downloaded: number; total: number | null }
  | { kind: "applying" };

export interface DownloadProgressProps {
  pending: string;
  /** The async work; call `report` as the phase changes. Resolves to the success line. */
  run: (report: (phase: DownloadPhase) => void) => Promise<string>;
  nerdfont: boolean;
  theme: Theme;
  onDone?: (ok: boolean) => void;
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes}B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)}MB`;
}

/** The download sub-line: a bar filled in the success colour up to the fraction done. */
function DownloadBar({
  phase,
  theme,
}: {
  phase: Extract<DownloadPhase, { kind: "downloading" }>;
  theme: Theme;
}) {
  const fill = inkColor(theme.color("success"));
  const track = inkColor(theme.color("text-secondary"));

  if (phase.total === null) {
    return (
      <Box>
        <Text>downloading new version... </Text>
        <Text color={fill}>{formatBytes(phase.downloaded)}</Text>
      </Box>
    );
  }

  const fraction = phase.total === 0 ? 1 : Math.min(1, phase.downloaded / phase.total);
  const filled = Math.round(fraction * BAR_WIDTH);
  return (
    <Box>
      <Text>downloading new version... </Text>
      <Text color={fill}>{"─".repeat(filled)}</Text>
      <Text color={track}>{"─".repeat(BAR_WIDTH - filled)}</Text>
      <Text> {Math.round(fraction * 100)}% </Text>
      <Text color={track}>
        ({formatBytes(phase.downloaded)}/{formatBytes(phase.total)})
      </Text>
    </Box>
  );
}

export function DownloadProgress({ pending, run, nerdfont, theme, onDone }: DownloadProgressProps) {
  const [status, setStatus] = useState<Status>({ kind: "running" });
  const [frame, setFrame] = useState(0);
  const [phase, setPhase] = useState<DownloadPhase | null>(null);

  useEffect(() => {
    if (status.kind !== "running") return;
    const id = setInterval(() => setFrame((f) => (f + 1) % FRAMES.length), FRAME_MS);
    return () => clearInterval(id);
  }, [status.kind]);

  useEffect(() => {
    let live = true;
    run((next) => {
      if (live) setPhase(next);
    }).then(
      (message) => {
        if (live) setStatus({ kind: "success", message });
      },
      (error: unknown) => {
        if (!live) return;
        const message = error instanceof Error ? error.message : String(error);
        setStatus({ kind: "failure", message });
      },
    );
    return () => {
      live = false;
    };
    // Run once on mount; `run` is expected to be a stable operation.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // See Spinner for why this fires in a layout effect rather than the settling
  // effect itself: it keeps onDone from racing the final frame onto the screen.
  useLayoutEffect(() => {
    if (status.kind !== "running") onDone?.(status.kind === "success");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status]);

  if (status.kind !== "running") {
    const glyphs = createGlyphs(nerdfont);
    const glyph = status.kind === "success" ? glyphs.success : glyphs.failure;
    return (
      <Box>
        <Text>{glyph} </Text>
        <Text>{status.message}</Text>
      </Box>
    );
  }

  return (
    <Box flexDirection="column">
      <Box>
        <Text>{FRAMES[frame]} </Text>
        <Text>{pending}</Text>
      </Box>
      {phase?.kind === "downloading" && <DownloadBar phase={phase} theme={theme} />}
      {phase?.kind === "applying" && <Text>applying update...</Text>}
    </Box>
  );
}
