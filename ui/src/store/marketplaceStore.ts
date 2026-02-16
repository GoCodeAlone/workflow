// ---------------------------------------------------------------------------
// Marketplace store — thin wrapper around pluginStore for the Marketplace UI.
//
// All plugin data (enabled/disabled, versions, dependencies) lives in
// pluginStore. This store only manages marketplace-specific UI state
// (search filtering, selected plugin, action-in-progress flags).
// ---------------------------------------------------------------------------

import { create } from 'zustand';

interface MarketplaceStore {
  searchQuery: string;
  error: string | null;

  setSearchQuery: (q: string) => void;
  clearError: () => void;
}

const useMarketplaceStore = create<MarketplaceStore>((set) => ({
  searchQuery: '',
  error: null,

  setSearchQuery: (q: string) => set({ searchQuery: q }),
  clearError: () => set({ error: null }),
}));

export default useMarketplaceStore;
