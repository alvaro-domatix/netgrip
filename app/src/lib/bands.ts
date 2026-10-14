/**
 * Banda de una radio wifi: los probes del backend usan "2g" | "5g" | "6g".
 * Helper único para etiquetarla: antes cada componente hacía
 * `band === "5g" ? band5 : band24` y una radio 6 GHz salía como 2.4 GHz.
 */
export type WifiBand = "2g" | "5g" | "6g";

/** Clave i18n de la etiqueta corta de la banda ("2.4 GHz" / "5 GHz" / "6 GHz"). */
export function bandLabelKey(band: string | undefined): "wifi.band24" | "wifi.band5" | "wifi.band6" {
  if (band === "6g") return "wifi.band6";
  if (band === "5g") return "wifi.band5";
  return "wifi.band24";
}
