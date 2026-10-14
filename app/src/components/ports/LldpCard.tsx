import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Radar } from "lucide-react";
import { api } from "../../api";
import type { LldpProbe } from "../../types";
import { ActionBanner, Button, Card, Pill, SkeletonRows, Toggle } from "../ui";
import { useActionCycle } from "../wifi/action";

/**
 * Vecinos LLDP/CDP (#485-audit): quién hay al otro lado de cada boca, leído
 * de lldpd. Sin lldpd instalado la tarjeta ofrece activarlo (instala el
 * paquete vía executor) en vez de desaparecer: es la pieza que MikroTik
 * muestra en IP > Neighbors y UniFi en el panel de boca.
 */
export function LldpCard({ index = 0 }: { index?: number }) {
  const { t } = useTranslation();
  const [probe, setProbe] = useState<LldpProbe>();
  const { phase, detail, busy, run, clear } = useActionCycle();

  const load = useCallback(() => {
    api.lldp().then(setProbe).catch(() => {});
  }, []);

  useEffect(() => { load(); }, [load]);

  const toggle = async (enabled: boolean) => {
    const res = await run(() => api.setLldp(enabled));
    if (res) setProbe(res.state);
  };

  return (
    <Card index={index} icon={Radar} title={t("lldp.title")} help="lldp">
      {phase && (
        <div className="mb-2">
          <ActionBanner phase={phase} detail={detail} onDone={clear} />
        </div>
      )}
      {!probe ? (
        <SkeletonRows rows={3} />
      ) : !probe.installed ? (
        <div className="flex flex-col items-start gap-2">
          <p className="text-small text-muted">{t("lldp.notInstalled")}</p>
          <Button variant="secondary" size="sm" disabled={busy} onClick={() => toggle(true)}>
            {t("lldp.install")}
          </Button>
        </div>
      ) : (
        <>
          <div className="flex items-center justify-between gap-3">
            <div className="flex items-center gap-2">
              <Toggle checked={probe.enabled && probe.running} busy={busy} onChange={toggle} label={t("lldp.title")} />
              <span className="text-small text-muted">{t("lldp.daemon")}</span>
            </div>
            <Pill tone={probe.running ? "ok" : "muted"}>
              {probe.running ? t("lldp.running") : t("lldp.stopped")}
            </Pill>
          </div>
          {!probe.running ? (
            <p className="text-small text-muted mt-3">{t("lldp.stoppedHint")}</p>
          ) : probe.neighbors.length === 0 ? (
            <p className="text-small text-muted mt-3">{t("lldp.empty")}</p>
          ) : (
            <div className="mt-3 divide-y divide-border/60">
              {probe.neighbors.map((n, i) => (
                <div key={`${n.interface}-${n.chassis_id}-${i}`} className="flex items-center gap-3 py-2.5">
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="font-medium truncate">{n.chassis_name || n.chassis_id}</span>
                      <Pill tone="accent">{n.protocol}</Pill>
                      {n.mgmt_ip && <Pill tone="muted">{n.mgmt_ip}</Pill>}
                    </div>
                    <p className="text-small text-muted mt-0.5 truncate">
                      {t("lldp.onPort", { port: n.interface })}
                      {n.port_id ? ` · ${t("lldp.theirPort", { port: n.port_id })}` : ""}
                      {n.sys_descr ? ` · ${n.sys_descr}` : ""}
                    </p>
                  </div>
                </div>
              ))}
            </div>
          )}
        </>
      )}
    </Card>
  );
}
