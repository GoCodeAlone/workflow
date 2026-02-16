import { create } from 'zustand';

const V1_BASE = '/api/v1';

function getAuthHeaders(): Record<string, string> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  const token = localStorage.getItem('auth_token');
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }
  return headers;
}

async function docFetch<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${V1_BASE}${path}`, {
    ...options,
    headers: { ...getAuthHeaders(), ...(options?.headers as Record<string, string>) },
  });
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText);
    throw new Error(`API ${res.status}: ${text}`);
  }
  if (res.status === 204) return undefined as T;
  return res.json();
}

export interface Doc {
  id: string;
  workflow_id?: string;
  title: string;
  content: string;
  category: string;
  created_by?: string;
  updated_by?: string;
  created_at: string;
  updated_at: string;
}

interface DocManagerStore {
  // Doc list
  docs: Doc[];
  selectedDoc: Doc | null;
  categories: string[];
  loading: boolean;

  // Filters
  filters: { workflow_id?: string; category?: string; search?: string };
  setFilter: (key: string, value: string) => void;

  // CRUD
  fetchDocs: () => Promise<void>;
  fetchDoc: (id: string) => Promise<void>;
  createDoc: (doc: { title: string; content: string; workflow_id?: string; category?: string }) => Promise<void>;
  updateDoc: (id: string, doc: { title?: string; content?: string; category?: string }) => Promise<void>;
  deleteDoc: (id: string) => Promise<void>;
  fetchCategories: () => Promise<void>;

  // Editor state
  editorContent: string;
  editorDirty: boolean;
  setEditorContent: (content: string) => void;
  selectDoc: (doc: Doc | null) => void;
}

const PLUGIN_BASE = '/admin/plugins/doc-manager';

const useDocManagerStore = create<DocManagerStore>((set, get) => ({
  docs: [],
  selectedDoc: null,
  categories: [],
  loading: false,
  filters: {},
  editorContent: '',
  editorDirty: false,

  setFilter: (key, value) => {
    set((state) => ({
      filters: { ...state.filters, [key]: value || undefined },
    }));
  },

  fetchDocs: async () => {
    set({ loading: true });
    try {
      const { filters } = get();
      const params = new URLSearchParams();
      if (filters.workflow_id) params.set('workflow_id', filters.workflow_id);
      if (filters.category) params.set('category', filters.category);
      if (filters.search) params.set('search', filters.search);
      const qs = params.toString();
      const docs = await docFetch<Doc[]>(`${PLUGIN_BASE}/docs${qs ? '?' + qs : ''}`);
      set({ docs, loading: false });
    } catch {
      set({ docs: [], loading: false });
    }
  },

  fetchDoc: async (id) => {
    try {
      const doc = await docFetch<Doc>(`${PLUGIN_BASE}/docs/${encodeURIComponent(id)}`);
      set({ selectedDoc: doc, editorContent: doc.content, editorDirty: false });
    } catch {
      // ignore
    }
  },

  createDoc: async (doc) => {
    await docFetch<Doc>(`${PLUGIN_BASE}/docs`, {
      method: 'POST',
      body: JSON.stringify(doc),
    });
    await get().fetchDocs();
  },

  updateDoc: async (id, doc) => {
    const updated = await docFetch<Doc>(`${PLUGIN_BASE}/docs/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(doc),
    });
    set({ selectedDoc: updated, editorContent: updated.content, editorDirty: false });
    await get().fetchDocs();
  },

  deleteDoc: async (id) => {
    await docFetch<void>(`${PLUGIN_BASE}/docs/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    });
    const { selectedDoc } = get();
    if (selectedDoc?.id === id) {
      set({ selectedDoc: null, editorContent: '', editorDirty: false });
    }
    await get().fetchDocs();
  },

  fetchCategories: async () => {
    try {
      const categories = await docFetch<string[]>(`${PLUGIN_BASE}/categories`);
      set({ categories });
    } catch {
      set({ categories: [] });
    }
  },

  setEditorContent: (content) => {
    const { selectedDoc } = get();
    set({
      editorContent: content,
      editorDirty: selectedDoc ? content !== selectedDoc.content : content !== '',
    });
  },

  selectDoc: (doc) => {
    set({
      selectedDoc: doc,
      editorContent: doc?.content ?? '',
      editorDirty: false,
    });
  },
}));

export default useDocManagerStore;
