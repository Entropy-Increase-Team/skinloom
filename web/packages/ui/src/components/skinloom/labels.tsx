'use client'

import { createContext, useContext } from 'react'
import type * as React from 'react'

/**
 * Words the shared components show by themselves. The host app provides them in the
 * visitor's language (from its messages, namespace "ui"), so plugins get the same words
 * without translating them again. {name} placeholders are filled by fill().
 */
export interface UiLabels {
  copy: string
  copied: string
  cancel: string
  /** "{text}" is what must be typed, e.g. the account's email. */
  typeToConfirm: string
  /** "{count}" settings waiting to be saved. */
  unsaved: string
  discard: string
  save: string
  dropHint: string
  choose: string
  pause: string
  play: string
  previous: string
  next: string
  /** "{current}" of "{total}" in a carousel. */
  position: string
  private: string
  likes: string
  search: string
  noMatch: string
  all: string
  skins: string
  capes: string
}

export const defaultLabels: UiLabels = {
  copy: 'Copy',
  copied: 'Copied',
  cancel: 'Cancel',
  typeToConfirm: 'Type {text} to confirm',
  unsaved: '{count} unsaved change(s)',
  discard: 'Discard',
  save: 'Save',
  dropHint: 'Drop a file here, or',
  choose: 'choose one',
  pause: 'Pause',
  play: 'Play',
  previous: 'Previous',
  next: 'Next',
  position: '{current} / {total}',
  private: 'Private',
  likes: 'Favourites',
  search: 'Search',
  noMatch: 'Nothing matches',
  all: 'All',
  skins: 'Skins',
  capes: 'Capes',
}

const LabelsContext = createContext<UiLabels>(defaultLabels)

export function UiLabelsProvider({ labels, children }: { labels: Partial<UiLabels>; children: React.ReactNode }) {
  return <LabelsContext.Provider value={{ ...defaultLabels, ...labels }}>{children}</LabelsContext.Provider>
}

export function useUiLabels(): UiLabels {
  return useContext(LabelsContext)
}

/** Replaces {name} placeholders with values. */
export function fill(template: string, values: Record<string, string | number>): string {
  return template.replace(/\{(\w+)\}/g, (m, k: string) => (k in values ? String(values[k]) : m))
}
