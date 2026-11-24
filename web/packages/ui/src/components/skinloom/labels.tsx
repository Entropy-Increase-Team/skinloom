'use client'

import { createContext, useContext, useMemo } from 'react'
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
  close: string
  loading: string
  more: string
  morePages: string
  previousPage: string
  nextPage: string
  toggleSidebar: string
  navigation: string
  mode3d: string
  mode2d: string
  walk: string
  run: string
  fly: string
  idle: string
  autoRotate: string
  resetView: string
  background: string
  bgTransparent: string
  bgWhite: string
  bgGray: string
  bgBlack: string
  backEquipment: string
  breadcrumb: string
  pagination: string
  clear: string
  remove: string
  openOptions: string
  commandPalette: string
  commandSearch: string
  unsavedChanges: string
  previewUnavailable: string
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
  likes: 'Likes',
  search: 'Search',
  noMatch: 'Nothing matches',
  all: 'All',
  skins: 'Skins',
  capes: 'Capes',
  close: 'Close',
  loading: 'Loading',
  more: 'More',
  morePages: 'More pages',
  previousPage: 'Go to the previous page',
  nextPage: 'Go to the next page',
  toggleSidebar: 'Show or hide the sidebar',
  navigation: 'Navigation',
  mode3d: '3D',
  mode2d: '2D',
  walk: 'Walk',
  run: 'Run',
  fly: 'Fly',
  idle: 'Idle',
  autoRotate: 'Auto rotate',
  resetView: 'Reset view',
  background: 'Preview background',
  bgTransparent: 'Transparent background',
  bgWhite: 'White background',
  bgGray: 'Gray background',
  bgBlack: 'Black background',
  backEquipment: 'Switch cape / elytra',
  breadcrumb: 'Breadcrumb',
  pagination: 'Pagination',
  clear: 'Clear',
  remove: 'Remove',
  openOptions: 'Toggle options',
  commandPalette: 'Command palette',
  commandSearch: 'Search for a command',
  unsavedChanges: 'Unsaved changes',
  previewUnavailable: '3D preview unavailable',
}

const LabelsContext = createContext<UiLabels>(defaultLabels)

export function UiLabelsProvider({ labels, children }: { labels: Partial<UiLabels>; children: React.ReactNode }) {
  const value = useMemo(() => ({ ...defaultLabels, ...labels }), [labels])
  return <LabelsContext.Provider value={value}>{children}</LabelsContext.Provider>
}

export function useUiLabels(): UiLabels {
  return useContext(LabelsContext)
}

/** Replaces {name} placeholders with values. */
export function fill(template: string, values: Record<string, string | number>): string {
  return template.replace(/\{(\w+)\}/g, (m, k: string) => (k in values ? String(values[k]) : m))
}

/** Words only screen readers hear (a close button's name…), in the visitor's language. */
export function SrLabel({ k }: { k: keyof UiLabels }) {
  const t = useUiLabels()
  return <span className="sr-only">{t[k]}</span>
}
