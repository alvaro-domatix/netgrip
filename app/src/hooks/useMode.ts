import { useEffect, useState } from "react";
import { api } from "../api";
import type { ModeProbe } from "../types";

/**
 * Rol del equipo con hidratación instantánea (#484): el primer paint usa el
 * último probe cacheado en localStorage y el probe real se refetcha en
 * segundo plano. Sin esto, hasta que llegaba /api/mode el nav se quedaba
 * permisivo y las entradas gateadas por rol parpadeaban al recargar.
 * En demo api.mode() delega en demoApi, así que la ruta es la misma.
 *
 * Store compartido a nivel de módulo: todos los consumidores (Shell,
 * Overview, System, ModeCard) leen el mismo valor y se hace UN solo fetch
 * por carga; refreshMode() invalida la caché y refetchea (tras cambiar el
 * modo router/AP), actualizando el nav sin recargar la página.
 */
const CACHE_KEY = "netgrip.mode.v1";

/** Validación defensiva del shape mínimo cacheado: mode/has_wifi/port_count
 *  siempre presentes; role solo se acepta si es un rol conocido. */
function readCache(): ModeProbe | undefined {
  try {
    const raw = localStorage.getItem(CACHE_KEY);
    if (!raw) return undefined;
    const v = JSON.parse(raw) as Record<string, unknown>;
    if (!v || typeof v !== "object") return undefined;
    if (v.mode !== "router" && v.mode !== "ap") return undefined;
    if (typeof v.has_wifi !== "boolean") return undefined;
    if (typeof v.port_count !== "number" || v.port_count < 0) return undefined;
    if (v.role !== undefined && v.role !== "router" && v.role !== "ap" && v.role !== "switch") {
      return undefined;
    }
    return v as unknown as ModeProbe;
  } catch {
    return undefined;
  }
}

let current: ModeProbe | undefined = readCache();
let fetchStarted = false;
const listeners = new Set<() => void>();

function notify() {
  for (const l of listeners) l();
}

function fetchMode() {
  api
    .mode()
    .then((m) => {
      current = m;
      try {
        localStorage.setItem(CACHE_KEY, JSON.stringify(m));
      } catch {
        // Sin localStorage (modo privado) el gating sigue funcionando
        // con el probe; solo se pierde la hidratación instantánea.
      }
      notify();
    })
    .catch(() => {
      // Probe caído: se conserva la caché si la hay. Sin caché el rol
      // queda desconocido y el nav se queda en lo no gateado (overview).
    });
}

/** Invalida la caché y refetchea el probe: tras un cambio de modo el rol
 *  nuevo llega a todos los consumidores sin recargar. */
export function refreshMode() {
  try {
    localStorage.removeItem(CACHE_KEY);
  } catch {
    // Sin localStorage no hay caché que invalidar.
  }
  fetchMode();
}

/** { mode, modeReady }: modeReady = hay un valor fiable (caché o probe).
 *  Hasta entonces los consumidores deben tratar el rol como desconocido y
 *  no pintar nada gateado por rol. */
export function useMode(): { mode: ModeProbe | undefined; modeReady: boolean } {
  const [mode, setMode] = useState<ModeProbe | undefined>(current);

  useEffect(() => {
    const listener = () => setMode(current);
    listeners.add(listener);
    if (!fetchStarted) {
      fetchStarted = true;
      fetchMode();
    }
    // Por si otro consumidor ya completó el fetch antes del montaje.
    setMode(current);
    return () => {
      listeners.delete(listener);
    };
  }, []);

  return { mode, modeReady: mode !== undefined };
}
