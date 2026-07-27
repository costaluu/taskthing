import { useEffect, useLayoutEffect, useState } from "react";
import { Box, Text } from "ink";

import { createGlyphs } from "./glyph";
import type { Theme } from "./theme";

// ── spinner ──────────────────────────────────────────────────────────────────
//
// A live status line for the slow, async commands (story 10-13). It animates a
// braille frame beside a pending message, then settles into a themed success or
// failure line. Generic on purpose: `run` resolves to the finished success line
// (so update-check's three outcomes need no special-casing) or rejects with an
// Error whose message is the already-summarized failure line. The per-command
// copy lives in its own module.

// The braille frames animate the same way with or without nerdfont, so they are
// not part of the glyph module's nerdfont/fallback split.
const FRAMES = ["⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"];
const FRAME_MS = 80;

type Status =
  | { kind: "running" }
  | { kind: "success"; message: string }
  | { kind: "failure"; message: string };

export interface SpinnerProps {
  /** In-progress message, e.g. `syncing <workspace>...`. */
  pending: string;
  /** The async work: resolves to the success line, rejects with a failure line. */
  run: () => Promise<string>;
  nerdfont: boolean;
  theme: Theme;
  onDone?: (ok: boolean) => void;
}

export function Spinner({ pending, run, nerdfont, onDone }: SpinnerProps) {
  const [status, setStatus] = useState<Status>({ kind: "running" });
  const [frame, setFrame] = useState(0);

  // The braille frame animates only while running; once settled it stops, so no
  // stray tick re-renders (or keeps a timer alive) after the final line is drawn.
  useEffect(() => {
    if (status.kind !== "running") return;
    const id = setInterval(() => setFrame((f) => (f + 1) % FRAMES.length), FRAME_MS);
    return () => clearInterval(id);
  }, [status.kind]);

  useEffect(() => {
    let live = true;
    run().then(
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

  // Signal completion only *after* the settled line has been written (item 15):
  // callers unmount on this signal, and unmounting in the same tick as the state
  // update would race the re-render and freeze the pending "⣾ …" frame on screen
  // instead of the result. A layout effect runs synchronously right after ink
  // commits the frame (it writes in resetAfterCommit, before layout effects), so
  // the result is on screen before onDone, yet onDone still fires in the same
  // flush — no yield for an unmount to slip into.
  useLayoutEffect(() => {
    if (status.kind !== "running") onDone?.(status.kind === "success");
    // onDone is stable for a given mount (the element is created once); keying on
    // status alone fires it exactly once, when the spinner settles.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status]);

  if (status.kind === "running") {
    return (
      <Box>
        <Text>{FRAMES[frame]} </Text>
        <Text>{pending}</Text>
      </Box>
    );
  }

  const glyphs = createGlyphs(nerdfont);
  const glyph = status.kind === "success" ? glyphs.success : glyphs.failure;
  return (
    <Box>
      <Text>{glyph} </Text>
      <Text>{status.message}</Text>
    </Box>
  );
}
