import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import es from "./locales/es";
import en from "./locales/en";

const stored = (() => {
  // Una sola clave para la ELECCIÓN del usuario: netgrip:lang (es/en/auto).
  // Migración desde la clave antigua (netgrip-lang, que guardaba el idioma
  // resuelto): se adopta su valor y se borra. Guardar el resuelto rompía el
  // modo "auto", que debe seguir al navegador en cada carga.
  try {
    const cur = localStorage.getItem("netgrip:lang");
    if (cur === "es" || cur === "en") return cur;
    const old = localStorage.getItem("netgrip-lang");
    if (old === "es" || old === "en") {
      localStorage.setItem("netgrip:lang", old);
      localStorage.removeItem("netgrip-lang");
      return old;
    }
  } catch {
    // Sin localStorage (modo privado): se usa el navegador.
  }
  return undefined;
})();
const browser = navigator.language.startsWith("es") ? "es" : "en";

i18n.use(initReactI18next).init({
  resources: { es: { translation: es }, en: { translation: en } },
  lng: stored || browser,
  fallbackLng: "en",
  interpolation: { escapeValue: false },
});

// The document's lang has to follow the active language. Served as a static
// "es" it told every browser the page was Spanish, so an English UI was
// offered for translation on every load - and accepting that runs the page
// through a translator, which rewrites the data too: interface names,
// hostnames and SSIDs are not words to translate.
const applyDocLang = (lng?: string) => {
  const l = (lng ?? i18n.language ?? "en").slice(0, 2).toLowerCase();
  document.documentElement.lang = l === "es" ? "es" : "en";
};
applyDocLang();

i18n.on("languageChanged", (lng) => {
  // La persistencia de la elección la escribe useLang (netgrip:lang);
  // aquí NO se escribe el idioma resuelto: convertiría "auto" en un
  // idioma fijo y el navegador dejaría de mandar en la siguiente carga.
  applyDocLang(lng);
});

export default i18n;
