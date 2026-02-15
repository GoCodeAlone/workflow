import { create } from 'zustand';
import { persist } from 'zustand/middleware';

interface PanelWidths {
  projectSwitcher: number;
  nodePalette: number;
  propertyPanel: number;
}

interface UILayoutStore {
  projectSwitcherCollapsed: boolean;
  nodePaletteCollapsed: boolean;
  propertyPanelCollapsed: boolean;

  panelWidths: PanelWidths;

  toggleProjectSwitcher: () => void;
  toggleNodePalette: () => void;
  togglePropertyPanel: () => void;

  setProjectSwitcherCollapsed: (collapsed: boolean) => void;
  setNodePaletteCollapsed: (collapsed: boolean) => void;
  setPropertyPanelCollapsed: (collapsed: boolean) => void;

  setPanelWidth: (panel: keyof PanelWidths, width: number) => void;
}

const DEFAULT_WIDTHS: PanelWidths = {
  projectSwitcher: 200,
  nodePalette: 240,
  propertyPanel: 280,
};

const PANEL_WIDTH_LIMITS: Record<keyof PanelWidths, { min: number; max: number }> = {
  projectSwitcher: { min: 150, max: 350 },
  nodePalette: { min: 180, max: 400 },
  propertyPanel: { min: 200, max: 500 },
};

export { PANEL_WIDTH_LIMITS };

const useUILayoutStore = create<UILayoutStore>()(
  persist(
    (set, get) => ({
      projectSwitcherCollapsed: false,
      nodePaletteCollapsed: false,
      propertyPanelCollapsed: false,

      panelWidths: { ...DEFAULT_WIDTHS },

      toggleProjectSwitcher: () =>
        set({ projectSwitcherCollapsed: !get().projectSwitcherCollapsed }),
      toggleNodePalette: () =>
        set({ nodePaletteCollapsed: !get().nodePaletteCollapsed }),
      togglePropertyPanel: () =>
        set({ propertyPanelCollapsed: !get().propertyPanelCollapsed }),

      setProjectSwitcherCollapsed: (collapsed) =>
        set({ projectSwitcherCollapsed: collapsed }),
      setNodePaletteCollapsed: (collapsed) =>
        set({ nodePaletteCollapsed: collapsed }),
      setPropertyPanelCollapsed: (collapsed) =>
        set({ propertyPanelCollapsed: collapsed }),

      setPanelWidth: (panel, width) => {
        const limits = PANEL_WIDTH_LIMITS[panel];
        const clamped = Math.round(Math.max(limits.min, Math.min(limits.max, width)));
        set({
          panelWidths: { ...get().panelWidths, [panel]: clamped },
        });
      },
    }),
    {
      name: 'workflow-ui-layout',
    },
  ),
);

export default useUILayoutStore;
