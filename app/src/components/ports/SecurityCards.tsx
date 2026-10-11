import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Lock, Pin, ShieldCheck, X } from "lucide-react";
import { api } from "../../api";
import type { PortIsolationProbe, PortSecurityProbe } from "../../types";
import { Banner, Button, Card, ConfirmDialog, EmptyState, Pill, Toggle } from "../ui";
import { CardLoadError } from "../tools/diagnostics";
import { PortAddPicker } from "./PortOverrides";

/**
 * Pestaña "Perfiles y seguridad" (#449): aislamiento de bocas del puente
 * (sysfs brport/isolated) y seguridad de boca (aprendizaje on/off + MACs
 * fijas FDB). Sin tc ni paquetes extra: flags del bridge del kernel. El
 * límite de MACs por boca no lo soporta el kernel de ningún target: el
 * probe lo reporta y la card lo dice limpio.
 */

const LAN_FIRST = (a: { port: string }, b: { port: string }) =>
  a.port.localeCompare(b.port, undefined, { numeric: true });

/** Aislamiento de bocas (`/api/port-isolation`): lista + picker, aplicado al momento. */
export function PortIsolationCard() {
  const { t } = useTranslation();
  const [probe, setProbe] = useState<PortIsolationProbe>();
  const [error, setError] = useState(false);
  const [busyPort, setBusyPort] = useState<string>();
  const [failMsg, setFailMsg] = useState<string>();

  const load = useCallback(async () => {
    setError(false);
    try {
      const p = await api.portIsolation();
      setProbe(p);
    } catch { setError(true); }
  }, []);
  useEffect(() => { load(); }, [load]);

  if (error) {
    return (
      <Card variant="subtle" animate={false} title={t("ports.isoTitle")} icon={Lock}>
        <CardLoadError onRetry={load} />
      </Card>
    );
  }
  if (!probe?.applicable) return null;

  if (probe.isolated_supported === false) {
    return (
      <Card variant="subtle" animate={false} title={t("ports.isoTitle")} icon={Lock}>
        <p className="text-small text-muted mb-3">{t("ports.isoDesc")}</p>
        <EmptyState small title={t("ports.isoUnsupported")} />
      </Card>
    );
  }

  const wired = [...probe.ports].sort(LAN_FIRST);
  // Bocas de la lista: aisladas o configuradas (pendientes de re-aplicar).
  const listed = wired.filter((p) => p.isolated || p.configured);
  const listedSet = new Set(listed.map((p) => p.port));
  const candidates = wired.filter((p) => !listedSet.has(p.port)).map((p) => p.port);

  const set = async (port: string, isolated: boolean) => {
    setBusyPort(port);
    setFailMsg(undefined);
    try {
      await api.setPortIsolation(port, isolated);
      await load();
    } catch (e) {
      setFailMsg(`${port}: ${e instanceof Error ? e.message : String(e)}`);
    } finally { setBusyPort(undefined); }
  };

  return (
    <Card variant="subtle" animate={false} title={t("ports.isoTitle")} icon={Lock} help="port-isolation">
      <p className="text-small text-muted mb-3">{t("ports.isoDesc")}</p>
      {listed.length === 0 ? (
        <p className="text-caption text-muted mb-2">{t("ports.isoEmpty")}</p>
      ) : (
        <div className="flex flex-col gap-1.5 mb-2">
          {listed.map((p) => (
            <div key={p.port} className="flex items-center gap-2 sm:gap-3">
              <span className="w-14 shrink-0 font-mono text-small">{p.port}</span>
              {p.isolated
                ? <Pill tone="ok">{t("ports.isoActive")}</Pill>
                : <Pill tone="warn">{t("ports.isoPending")}</Pill>}
              <button
                type="button"
                onClick={() => void set(p.port, false)}
                disabled={busyPort === p.port}
                title={`${t("ports.removeOverride")} (${p.port})`}
                aria-label={`${t("ports.removeOverride")} (${p.port})`}
                className="ml-auto inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-faint hover:text-danger transition-colors duration-[var(--dur-fast)] ring-focus disabled:opacity-50"
              >
                <X size={16} aria-hidden="true" />
              </button>
            </div>
          ))}
        </div>
      )}
      <PortAddPicker candidates={candidates} onAdd={(port) => void set(port, true)} />
      {failMsg && <Banner tone="danger" className="mt-2" onDismiss={() => setFailMsg(undefined)}>{failMsg}</Banner>}
    </Card>
  );
}

/** Seguridad de boca (`/api/port-security`): aprendizaje, MACs fijas y aviso del límite de MACs. */
export function PortSecurityCard() {
  const { t } = useTranslation();
  const [probe, setProbe] = useState<PortSecurityProbe>();
  const [error, setError] = useState(false);
  const [busyPort, setBusyPort] = useState<string>();
  const [failMsg, setFailMsg] = useState<string>();
  // Añadir MAC fija: boca elegida + texto de la MAC (aplicado al momento).
  const [macPort, setMacPort] = useState("");
  const [macText, setMacText] = useState("");
  // Confirmación al desactivar el aprendizaje: sin MACs fijas la boca se queda ciega.
  const [confirmLearn, setConfirmLearn] = useState<string>();
  const [pendingLearning, setPendingLearning] = useState<string>();

  const load = useCallback(async () => {
    setError(false);
    try {
      const p = await api.portSecurity();
      setProbe(p);
    } catch { setError(true); }
  }, []);
  useEffect(() => { load(); }, [load]);

  if (error) {
    return (
      <Card variant="subtle" animate={false} title={t("ports.secTitle")} icon={ShieldCheck}>
        <CardLoadError onRetry={load} />
      </Card>
    );
  }
  if (!probe?.applicable) return null;

  const learningOK = probe.learning_supported !== false;
  const fdbOK = probe.fdb_supported !== false;

  const wired = [...probe.ports].sort(LAN_FIRST);
  // Lista del aprendizaje: bocas con el off configurado o ya aplicado.
  const learnListed = wired.filter((p) => p.learning_configured || !p.learning);
  const learnSet = new Set(learnListed.map((p) => p.port));
  const learnCandidates = wired.filter((p) => learnSet.has(p.port) === false).map((p) => p.port);
  const macPorts = wired.filter((p) => p.static_macs.length > 0);
  const macCandidates = wired.filter((p) => p.static_macs.length === 0).map((p) => p.port);

  const setLearning = async (port: string, learning: boolean) => {
    setBusyPort(port);
    setFailMsg(undefined);
    try {
      await api.setPortLearning(port, learning);
      await load();
    } catch (e) {
      setFailMsg(`${port}: ${e instanceof Error ? e.message : String(e)}`);
    } finally { setBusyPort(undefined); }
  };

  const requestLearningOff = (port: string) => {
    setPendingLearning(port);
    setConfirmLearn(port);
  };

  const addMac = async () => {
    if (!macPort || !macText.trim()) return;
    setBusyPort(macPort);
    setFailMsg(undefined);
    try {
      await api.setPortStaticMac(macPort, macText.trim(), true);
      setMacText("");
      await load();
    } catch (e) {
      setFailMsg(`${macPort}: ${e instanceof Error ? e.message : String(e)}`);
    } finally { setBusyPort(undefined); }
  };

  const removeMac = async (port: string, mac: string) => {
    setBusyPort(port);
    setFailMsg(undefined);
    try {
      await api.setPortStaticMac(port, mac, false);
      await load();
    } catch (e) {
      setFailMsg(`${port}: ${e instanceof Error ? e.message : String(e)}`);
    } finally { setBusyPort(undefined); }
  };

  return (
    <Card variant="subtle" animate={false} title={t("ports.secTitle")} icon={ShieldCheck} help="port-security">
      <p className="text-small text-muted mb-3">{t("ports.secDesc")}</p>

      {/* Aprendizaje de direcciones */}
      <div className="text-small font-medium mb-1">{t("ports.learnTitle")}</div>
      <p className="text-caption text-muted mb-2">{t("ports.learnDesc")}</p>
      {!learningOK ? (
        <EmptyState small title={t("ports.secLearnUnsupported")} />
      ) : (
        <>
          {learnListed.length === 0 ? (
            <p className="text-caption text-muted mb-2">{t("ports.learnEmpty")}</p>
          ) : (
            <div className="flex flex-col gap-1.5 mb-2">
              {learnListed.map((p) => (
                <div key={p.port} className="flex items-center gap-2 sm:gap-3">
                  <span className="w-14 shrink-0 font-mono text-small">{p.port}</span>
                  {!p.learning && p.learning_configured && <Pill tone="ok">{t("ports.learnOffPill")}</Pill>}
                  {!p.learning && !p.learning_configured && <Pill tone="warn">{t("ports.isoPending")}</Pill>}
                  <Toggle
                    checked={p.learning}
                    busy={busyPort === p.port}
                    onChange={(v) => { if (!v) requestLearningOff(p.port); else void setLearning(p.port, true); }}
                    label={t("ports.learnTitle")}
                  />
                </div>
              ))}
            </div>
          )}
          <PortAddPicker candidates={learnCandidates} onAdd={requestLearningOff} />
        </>
      )}

      {/* MACs fijas */}
      <div className="text-small font-medium mt-4 mb-1">{t("ports.fdbTitle")}</div>
      <p className="text-caption text-muted mb-2">{t("ports.fdbDesc")}</p>
      {!fdbOK ? (
        <EmptyState small title={t("ports.fdbUnsupported")} />
      ) : (
        <>
          {macPorts.length === 0 ? (
            <p className="text-caption text-muted mb-2">{t("ports.fdbEmpty")}</p>
          ) : (
            <div className="flex flex-col gap-1.5 mb-2">
              {macPorts.map((p) => (
                <div key={p.port} className="flex flex-wrap items-center gap-2 sm:gap-3">
                  <span className="w-14 shrink-0 font-mono text-small">{p.port}</span>
                  <div className="flex flex-wrap items-center gap-1.5">
                    {p.static_macs.map((mac) => (
                      <span key={mac} className="inline-flex items-center gap-1 rounded-sm border border-border bg-surface-2 px-2 py-0.5 font-mono text-caption">
                        {mac}
                        <button
                          type="button"
                          onClick={() => void removeMac(p.port, mac)}
                          disabled={busyPort === p.port}
                          title={`${t("ports.fdbRemove")} (${mac})`}
                          aria-label={`${t("ports.fdbRemove")} (${mac})`}
                          className="inline-flex h-5 w-5 items-center justify-center rounded-sm text-faint hover:text-danger transition-colors duration-[var(--dur-fast)] ring-focus disabled:opacity-50"
                        >
                          <X size={12} aria-hidden="true" />
                        </button>
                      </span>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          )}
          <div className="flex flex-wrap items-center gap-2">
            <select
              value={macPort}
              onChange={(e) => setMacPort(e.target.value)}
              aria-label={t("ports.pickPort")}
              className="h-9 w-44 max-w-full rounded-sm border border-border bg-surface-2 px-3 text-small outline-none focus:border-accent ring-focus"
            >
              <option value="">{t("ports.pickPort")}</option>
              {(macPort && !macCandidates.includes(macPort) ? [macPort, ...macCandidates] : macCandidates)
                .map((p) => <option key={p} value={p}>{p}</option>)}
            </select>
            <input
              type="text"
              value={macText}
              onChange={(e) => setMacText(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter") void addMac(); }}
              placeholder={t("ports.fdbMacPlaceholder")}
              aria-label={t("ports.fdbMacPlaceholder")}
              className="h-9 w-44 max-w-full rounded-sm border border-border bg-surface-2 px-3 font-mono text-small outline-none focus:border-accent ring-focus"
            />
            <Button variant="secondary" size="sm" icon={Pin} disabled={!macPort || !macText.trim() || busyPort === macPort} onClick={() => void addMac()}>
              {t("ports.fdbAdd")}
            </Button>
          </div>
        </>
      )}

      {/* Límite de MACs: el kernel del bridge no lo soporta, se dice limpio. */}
      <div className="text-small font-medium mt-4 mb-1">{t("ports.macLimitTitle")}</div>
      <p className="text-caption text-muted">{t("ports.macLimitUnsupported")}</p>

      {failMsg && <Banner tone="danger" className="mt-2" onDismiss={() => setFailMsg(undefined)}>{failMsg}</Banner>}
      <ConfirmDialog
        open={confirmLearn !== undefined}
        onClose={() => { setConfirmLearn(undefined); setPendingLearning(undefined); }}
        onConfirm={() => {
          const port = pendingLearning;
          setConfirmLearn(undefined);
          setPendingLearning(undefined);
          if (port) void setLearning(port, false);
        }}
        title={t("ports.learnOffTitle")}
        consequence={t("ports.learnOffConfirm", { port: confirmLearn ?? "" })}
        confirmLabel={t("ports.apply")}
        busy={busyPort !== undefined}
      />
    </Card>
  );
}
