"use client";

import {
  Box,
  Button,
  Checkbox,
  CloseButton,
  Dialog,
  Field,
  Input,
  NativeSelect,
  Portal,
  Progress,
  SimpleGrid,
  Stack,
  Text,
} from "@chakra-ui/react";
import { useEffect, useRef, useState } from "react";
import { LuDownload } from "react-icons/lu";

import { Callout } from "@/components/ui/callout";
import { toCsv } from "@/lib/csv";
import {
  progressShare,
  runExport,
  startProgress,
  type ExportDefinition,
  type ExportFilter,
  type ExportProgress,
  type FilterValues,
} from "@/lib/export";
import { formatCount } from "@/lib/format";
import { createRateLimiter, type RateLimiter } from "@/lib/rate-limit";

/** The backend gets at most five export reads a second. */
export const EXPORT_REQUESTS_PER_SECOND = 5;

/** Hands the finished file to the browser as a download. */
export type SaveFile = (name: string, text: string) => void;

/** The browser download: a UTF-8 CSV with a byte-order mark, so Excel reads names in any script. */
export const saveFile: SaveFile = (name, text) => {
  const url = URL.createObjectURL(new Blob(["﻿", text], { type: "text/csv;charset=utf-8" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 0);
};

const defaultLimiter = () => createRateLimiter(EXPORT_REQUESTS_PER_SECOND);

type Phase = "setup" | "running" | "failed" | "done";

interface ExportModalProps<T, L> {
  open: boolean;
  onClose: () => void;
  definition: ExportDefinition<T, L>;
  /** A fresh limiter per export; tests pass one on a fake clock. */
  createLimiter?: () => RateLimiter;
  save?: SaveFile;
  /** The clock of the file name. */
  now?: () => Date;
}

function FilterInput({ filter, value, onChange }: { filter: ExportFilter; value: string | boolean; onChange: (v: string | boolean) => void }) {
  switch (filter.kind) {
    case "select":
      return (
        <Field.Root>
          <Field.Label fontSize="13px">{filter.label}</Field.Label>
          <NativeSelect.Root size="sm">
            <NativeSelect.Field value={String(value)} onChange={(e) => onChange(e.currentTarget.value)}>
              {filter.options.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </NativeSelect.Field>
            <NativeSelect.Indicator />
          </NativeSelect.Root>
        </Field.Root>
      );
    case "amount":
      return (
        <Field.Root>
          <Field.Label fontSize="13px">{filter.label}</Field.Label>
          <Input
            size="sm"
            inputMode="decimal"
            placeholder={filter.placeholder}
            value={String(value)}
            onChange={(e) => onChange(e.currentTarget.value)}
          />
          {filter.hint && <Field.HelperText>{filter.hint}</Field.HelperText>}
        </Field.Root>
      );
    case "checkbox":
      return (
        <Checkbox.Root size="sm" alignItems="flex-start" checked={value === true} onCheckedChange={(e) => onChange(e.checked === true)}>
          <Checkbox.HiddenInput />
          <Checkbox.Control mt="2px" />
          <Checkbox.Label fontWeight="400">
            <Text as="span" fontSize="13px">
              {filter.label}
            </Text>
            {filter.hint && (
              <Text fontSize="12px" color="fg.muted">
                {filter.hint}
              </Text>
            )}
          </Checkbox.Label>
        </Checkbox.Root>
      );
  }
}

/**
 * The CSV export of a list: what the file holds in plain words, the columns
 * to write, the list's filters and a limit. Export reads the list under a
 * rate limiter with a progress bar, then offers the file to download.
 */
export function ExportModal<T, L>({ open, onClose, definition: d, createLimiter = defaultLimiter, save = saveFile, now = () => new Date() }: ExportModalProps<T, L>) {
  const [fields, setFields] = useState(() => new Set(d.fields.filter((f) => f.defaultOn !== false).map((f) => f.key)));
  const [filters, setFilters] = useState<FilterValues>(d.initialFilters);
  const [limitText, setLimitText] = useState(String(d.limit.initial));
  const [phase, setPhase] = useState<Phase>("setup");
  const [progress, setProgress] = useState<ExportProgress<T>>(startProgress);
  const abort = useRef<AbortController | null>(null);

  // Closing the modal (or leaving the page) stops a running export.
  useEffect(() => () => abort.current?.abort(), []);

  const limit = Number(limitText);
  const limitError =
    Number.isInteger(limit) && limit >= 1 && limit <= d.limit.max ? null : `Choose a whole number from 1 to ${formatCount(d.limit.max)}.`;
  const filterError = d.validate?.(filters) ?? null;
  const fieldsError = fields.size === 0 ? "Choose at least one column." : null;
  const source = d.source(filters);

  async function run(from: ExportProgress<T>) {
    const controller = new AbortController();
    abort.current = controller;
    setPhase("running");
    setProgress(from);
    try {
      const done = await runExport(source, { limit, limiter: createLimiter(), signal: controller.signal, from, onProgress: setProgress });
      if (controller.signal.aborted) return;
      setProgress(done);
      setPhase("done");
    } catch {
      if (!controller.signal.aborted) setPhase("failed");
    }
  }

  function stop() {
    abort.current?.abort();
    setPhase("setup");
    setProgress(startProgress());
  }

  function close() {
    abort.current?.abort();
    onClose();
  }

  function download() {
    const chosen = d.fields.filter((f) => fields.has(f.key));
    const rows = progress.items.flatMap((item) => d.lines(item)).map((line) => chosen.map((f) => f.value(line)));
    save(d.fileName(now()), toCsv(chosen.map((f) => f.header), rows));
  }

  const share = Math.round(progressShare(progress, limit, source.sizeHint ?? null) * 100);
  const lines = phase === "done" ? progress.items.reduce((n, item) => n + d.lines(item).length, 0) : 0;
  const busy = phase === "running" || phase === "failed";

  return (
    <Dialog.Root open={open} onOpenChange={(e) => !e.open && close()} size={{ base: "full", md: "lg" }} scrollBehavior="inside" lazyMount unmountOnExit>
      <Portal>
        <Dialog.Backdrop />
        <Dialog.Positioner>
          <Dialog.Content data-testid="export-modal">
            <Dialog.Header>
              <Dialog.Title>{d.title}</Dialog.Title>
            </Dialog.Header>
            <Dialog.Body>
              <Stack gap="5">
                <Callout title="What you get" data-testid="export-about">
                  {d.about}
                </Callout>

                {phase === "setup" && (
                  <>
                    <Box as="fieldset">
                      <Text as="legend" textStyle="label" mb="2">
                        Columns
                      </Text>
                      <SimpleGrid columns={{ base: 1, sm: 2 }} gap="2">
                        {d.fields.map((f) => (
                          <Checkbox.Root
                            key={f.key}
                            size="sm"
                            checked={fields.has(f.key)}
                            onCheckedChange={(e) =>
                              setFields((prev) => {
                                const next = new Set(prev);
                                if (e.checked === true) next.add(f.key);
                                else next.delete(f.key);
                                return next;
                              })
                            }
                          >
                            <Checkbox.HiddenInput />
                            <Checkbox.Control />
                            <Checkbox.Label fontSize="13px" fontWeight="400">
                              {f.header}
                            </Checkbox.Label>
                          </Checkbox.Root>
                        ))}
                      </SimpleGrid>
                      {fieldsError && (
                        <Text mt="2" fontSize="12px" color="neg.fg">
                          {fieldsError}
                        </Text>
                      )}
                    </Box>

                    <Stack gap="3">
                      <Text textStyle="label">Filters</Text>
                      {d.filters.map((f) => (
                        <FilterInput key={f.key} filter={f} value={filters[f.key] ?? ""} onChange={(v) => setFilters((prev) => ({ ...prev, [f.key]: v }))} />
                      ))}
                      {filterError && (
                        <Text fontSize="12px" color="neg.fg" data-testid="export-filter-error">
                          {filterError}
                        </Text>
                      )}
                      <Field.Root invalid={limitError != null}>
                        <Field.Label fontSize="13px">How many {d.noun} at most</Field.Label>
                        <Input size="sm" inputMode="numeric" value={limitText} onChange={(e) => setLimitText(e.currentTarget.value)} />
                        {limitError ? (
                          <Field.ErrorText>{limitError}</Field.ErrorText>
                        ) : (
                          <Field.HelperText>Up to {formatCount(d.limit.max)}.</Field.HelperText>
                        )}
                      </Field.Root>
                    </Stack>
                  </>
                )}

                {phase !== "setup" && (
                  <Stack gap="2" data-testid="export-progress">
                    <Progress.Root value={share} max={100} size="sm" colorPalette={phase === "failed" ? "red" : "blue"}>
                      <Progress.Track>
                        <Progress.Range />
                      </Progress.Track>
                    </Progress.Root>
                    <Text fontSize="13px" color="fg.muted" data-testid="export-status">
                      {phase === "running" && `Reading… ${formatCount(progress.items.length)} ${d.noun} so far.`}
                      {phase === "failed" &&
                        `The explorer did not answer. ${formatCount(progress.items.length)} ${d.noun} are read; try again to carry on.`}
                      {phase === "done" &&
                        `Ready: ${formatCount(progress.items.length)} ${d.noun}, ${formatCount(lines)} ${lines === 1 ? "line" : "lines"}.`}
                    </Text>
                  </Stack>
                )}
              </Stack>
            </Dialog.Body>
            <Dialog.Footer>
              {phase === "setup" && (
                <>
                  <Button variant="outline" size="sm" onClick={close}>
                    Cancel
                  </Button>
                  <Button
                    size="sm"
                    colorPalette="blue"
                    disabled={limitError != null || filterError != null || fieldsError != null}
                    onClick={() => void run(startProgress())}
                  >
                    Export
                  </Button>
                </>
              )}
              {busy && (
                <Button variant="outline" size="sm" onClick={stop}>
                  Stop
                </Button>
              )}
              {phase === "failed" && (
                <Button size="sm" colorPalette="blue" onClick={() => void run(progress)}>
                  Try again
                </Button>
              )}
              {phase === "done" && (
                <>
                  <Button variant="outline" size="sm" onClick={stop}>
                    Change and export again
                  </Button>
                  <Button size="sm" colorPalette="blue" onClick={download}>
                    <LuDownload /> Download CSV
                  </Button>
                </>
              )}
            </Dialog.Footer>
            <Dialog.CloseTrigger asChild>
              <CloseButton size="sm" aria-label="Close" />
            </Dialog.CloseTrigger>
          </Dialog.Content>
        </Dialog.Positioner>
      </Portal>
    </Dialog.Root>
  );
}

interface ExportButtonProps<T, L> extends Omit<ExportModalProps<T, L>, "open" | "onClose" | "definition"> {
  /** Built when the modal opens, so it starts from the list's current filters. */
  definition: () => ExportDefinition<T, L>;
  label?: string;
}

/** The "Export" button over a list and the modal it opens. */
export function ExportButton<T, L>({ definition, label = "Export", ...modal }: ExportButtonProps<T, L>) {
  const [opened, setOpened] = useState<ExportDefinition<T, L> | null>(null);
  return (
    <>
      <Button variant="outline" size="xs" h="30px" px="3" fontSize="12px" onClick={() => setOpened(definition())}>
        <LuDownload /> {label}
      </Button>
      {opened && <ExportModal open onClose={() => setOpened(null)} definition={opened} {...modal} />}
    </>
  );
}
