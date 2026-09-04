import type { StateCreator } from 'zustand'

export type Theme = 'light' | 'dark' | 'system'

export interface UISlice {
  // Sidebar
  sidebarCollapsed: boolean
  sidebarMobileOpen: boolean

  // Theme
  theme: Theme
  resolvedTheme: 'light' | 'dark'

  // Modals
  tagsModalOpen: boolean
  foldersModalOpen: boolean
  addEmailModalOpen: boolean
  shortcutsModalOpen: boolean
  commandPaletteOpen: boolean

  // AI assistant panel (right-side, persistent across routes)
  aiAssistantOpen: boolean

  // Unibox contact panel (null until the user chooses)
  uniboxContactPanelOpen: boolean | null

  // Actions - Sidebar
  toggleSidebar: () => void
  setSidebarCollapsed: (collapsed: boolean) => void
  setSidebarMobileOpen: (open: boolean) => void

  // Actions - Theme
  setTheme: (theme: Theme) => void
  setResolvedTheme: (theme: 'light' | 'dark') => void

  // Actions - Modals
  setTagsModalOpen: (open: boolean) => void
  setFoldersModalOpen: (open: boolean) => void
  setAddEmailModalOpen: (open: boolean) => void
  setShortcutsModalOpen: (open: boolean) => void
  setCommandPaletteOpen: (open: boolean) => void
  setAIAssistantOpen: (open: boolean) => void
  toggleAIAssistant: () => void
  setUniboxContactPanelOpen: (open: boolean) => void
}

const getInitialUniboxContactPanelOpen = (): boolean | null => {
  if (typeof window === 'undefined') return null
  try {
    const stored = window.localStorage.getItem('unibox.contactPanelOpen')
    return stored === 'true' ? true : stored === 'false' ? false : null
  } catch {
    return null
  }
}

const getInitialTheme = (): Theme => {
  if (typeof window === 'undefined') return 'system'
  return (localStorage.getItem('theme') as Theme) || 'system'
}

// The dashboard is light-only today: every surface is styled on white, so a
// resolved dark theme would flip only the CSS-variable components (command
// palette, toasts) and look broken. 'dark'/'system' are accepted but resolve
// to light until a real dark theme ships.
const getResolvedTheme = (_theme: Theme): 'light' | 'dark' => {
  return 'light'
}

export const createUISlice: StateCreator<UISlice, [], [], UISlice> = (set, get) => ({
  // Sidebar
  sidebarCollapsed: false,
  sidebarMobileOpen: false,

  // Theme
  theme: getInitialTheme(),
  resolvedTheme: getResolvedTheme(getInitialTheme()),

  // Modals
  tagsModalOpen: false,
  foldersModalOpen: false,
  addEmailModalOpen: false,
  shortcutsModalOpen: false,
  commandPaletteOpen: false,
  aiAssistantOpen: false,
  uniboxContactPanelOpen: getInitialUniboxContactPanelOpen(),

  // Actions - Sidebar
  toggleSidebar: () => set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),
  setSidebarCollapsed: (sidebarCollapsed) =>
    set((state) => (state.sidebarCollapsed === sidebarCollapsed ? state : { sidebarCollapsed })),
  setSidebarMobileOpen: (sidebarMobileOpen) =>
    set((state) => (state.sidebarMobileOpen === sidebarMobileOpen ? state : { sidebarMobileOpen })),

  // Actions - Theme
  setTheme: (theme) => {
    if (get().theme === theme) return
    localStorage.setItem('theme', theme)
    const resolvedTheme = getResolvedTheme(theme)
    document.documentElement.classList.remove('dark')
    set({ theme, resolvedTheme })
  },
  setResolvedTheme: (resolvedTheme) =>
    set((state) => (state.resolvedTheme === resolvedTheme ? state : { resolvedTheme })),

  // Actions - Modals
  setTagsModalOpen: (tagsModalOpen) =>
    set((state) => (state.tagsModalOpen === tagsModalOpen ? state : { tagsModalOpen })),
  setFoldersModalOpen: (foldersModalOpen) =>
    set((state) => (state.foldersModalOpen === foldersModalOpen ? state : { foldersModalOpen })),
  setAddEmailModalOpen: (addEmailModalOpen) =>
    set((state) => (state.addEmailModalOpen === addEmailModalOpen ? state : { addEmailModalOpen })),
  setShortcutsModalOpen: (shortcutsModalOpen) =>
    set((state) => (state.shortcutsModalOpen === shortcutsModalOpen ? state : { shortcutsModalOpen })),
  setCommandPaletteOpen: (commandPaletteOpen) =>
    set((state) => (state.commandPaletteOpen === commandPaletteOpen ? state : { commandPaletteOpen })),
  setAIAssistantOpen: (aiAssistantOpen) =>
    set((state) => (state.aiAssistantOpen === aiAssistantOpen ? state : { aiAssistantOpen })),
  toggleAIAssistant: () => set((state) => ({ aiAssistantOpen: !state.aiAssistantOpen })),
  setUniboxContactPanelOpen: (uniboxContactPanelOpen) => {
    if (typeof window !== 'undefined') {
      try {
        window.localStorage.setItem('unibox.contactPanelOpen', String(uniboxContactPanelOpen))
      } catch {
        // Keep the in-memory choice when storage is unavailable.
      }
    }
    set((state) => (state.uniboxContactPanelOpen === uniboxContactPanelOpen ? state : { uniboxContactPanelOpen }))
  },
})
