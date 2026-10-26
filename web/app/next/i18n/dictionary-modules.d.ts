// The per-language dictionaries the Vite config builds from app/locales (see `dictionaries` in vite.config.ts).
declare module 'virtual:dictionary/*' {
  const dictionary: Record<string, string | Partial<Record<Intl.LDMLPluralRule, string>>>;
  export default dictionary;
}
