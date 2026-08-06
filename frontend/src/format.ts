import {session} from '../wailsjs/go/models';

export function formatBytes(n: number): string {
    if (n <= 0) return "0 B";
    const units = ["B", "KB", "MB", "GB", "TB"];
    const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)));
    const v = n / Math.pow(1024, i);
    return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`;
}

export function formatRate(bps: number): string {
    return `${formatBytes(bps)}/s`;
}

// formatDuration renders seconds as the two largest non-zero units, e.g.
// "2d 4h", "3h 12m", "5m 3s", "41s". '∞' for non-finite/negative/absurd
// values rather than printing a nonsense number.
export function formatDuration(seconds: number): string {
    if (!Number.isFinite(seconds) || seconds < 0 || seconds > 365 * 24 * 3600) {
        return '∞';
    }
    const units: [number, string][] = [
        [86400, 'd'],
        [3600, 'h'],
        [60, 'm'],
        [1, 's'],
    ];
    const parts: string[] = [];
    let remaining = Math.floor(seconds);
    for (const [size, label] of units) {
        const n = Math.floor(remaining / size);
        if (n > 0) {
            parts.push(`${n}${label}`);
            remaining -= n * size;
        }
    }
    if (parts.length === 0) return '0s';
    return parts.slice(0, 2).join(' ');
}

// formatEta estimates time remaining from the summary. Approximate: remaining
// bytes are piece counts scaled by total size, so the short tail piece skews
// it slightly — fine for an ETA.
export function formatEta(summary: session.SessionSummary): string {
    if (summary.Status !== 'Running' || summary.TotalPieces === 0) return '—';
    const remaining = summary.TotalBytes * (1 - summary.DonePieces / summary.TotalPieces);
    if (remaining <= 0) return '—';
    if (summary.DownloadRate <= 0) return '∞';
    return formatDuration(remaining / summary.DownloadRate);
}

// formatRatio renders the share ratio (up / down). '∞' when uploading without
// having downloaded anything.
export function formatRatio(up: number, down: number): string {
    if (down <= 0) return up > 0 ? '∞' : '0.00';
    return (up / down).toFixed(2);
}

export function formatDate(unixSec: number): string {
    if (unixSec <= 0) return '—';
    return new Date(unixSec * 1000).toLocaleString();
}

// formatSwarm shows connected peers with the (possibly stale/empty) tracker
// swarm size in parentheses only when the tracker actually reported one.
export function formatSwarm(connected: number, swarm: number): string {
    return swarm > 0 ? `${connected} (${swarm})` : String(connected);
}

// formatTimeActive is the duration since StartedAt, ending at CompletedAt when
// set. Recomputed on every render (once per second via sessions:update), so no
// timer is needed.
export function formatTimeActive(summary: session.SessionSummary): string {
    if (summary.StartedAt === 0) return '—';
    const end = summary.CompletedAt !== 0 ? summary.CompletedAt : Date.now() / 1000;
    return formatDuration(end - summary.StartedAt);
}
