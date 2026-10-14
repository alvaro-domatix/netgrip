import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ListTree } from "lucide-react";
import { api } from "../../api";
import type { FdbProbe } from "../../types";
import { Card, Pill, SkeletonRows } from "../ui";

/**
 * Tabla MAC dinámica (FDB) del bridge: qué MACs ha aprendido cada boca y
 * con qué antigüedad. Es la host table de MikroTik y la MAC table que
 * UniFi enseña en el panel lateral de la boca. Read-only, agrupada por
 * boca, con filtro de texto (MAC o boca).
 */
export function MacTableCard({ index = 0 }: { index?: number }) {
  const { t } = useTranslation();
  const [probe, setProbe] = useState<FdbProbe>();
  const [filter, setFilter] = useState("");

  useEffect(() => {
    api.fdb().then(setProbe).catch(() => {});
  }, []);

  if (!probe) return <Card index={index} icon={ListTree} title={t("fdb.title")}><SkeletonRows rows={4} /></Card>;
  if (!probe.applicable) return null;

  const q = filter.trim().toLowerCase();
  const entries = probe.entries.filter(
    (e) => !q || e.mac.includes(q) || e.port.toLowerCase().includes(q),
  );
  const ports = [...new Set(entries.map((e) => e.port))];

  return (
    <Card
      index={index}
      icon={ListTree}
      title={t("fdb.title")}
      help="fdb"
      action={
        <input
          type="search"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder={t("fdb.filterPlaceholder")}
          aria-label={t("fdb.filterPlaceholder")}
          className="w-36 rounded-md border border-border bg-surface px-2 py-1 text-small"
        />
      }
    >
      {entries.length === 0 ? (
        <p className="text-small text-muted">{q ? t("fdb.emptyFilter") : t("fdb.empty")}</p>
      ) : (
        <div className="space-y-3">
          <p className="text-caption text-faint">{t("fdb.summary", { count: entries.length, ports: ports.length, bridge: probe.bridge })}</p>
          <div className="divide-y divide-border/60">
            {entries.map((e) => (
              <div key={`${e.port}-${e.mac}`} className="flex items-center gap-3 py-2">
                <span className="w-16 shrink-0 text-small font-medium">{e.port}</span>
                <span className="flex-1 font-mono text-small truncate">{e.mac}</span>
                {e.local && <Pill tone="muted">{t("fdb.local")}</Pill>}
                <span className="text-caption text-faint tabular-nums w-16 text-right">{e.ageing}s</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </Card>
  );
}
