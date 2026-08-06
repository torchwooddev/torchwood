/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_TORCHWOOD_ENDPOINT: string;
  readonly VITE_TORCHWOOD_PROJECT_ID: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
