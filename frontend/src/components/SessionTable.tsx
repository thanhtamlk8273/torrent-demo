import {session} from '../../wailsjs/go/models';
import {formatBytes, formatEta, formatRate, formatRatio, formatSwarm} from '../format';

interface Props {
    sessions: session.SessionSummary[];
    selectedId: string | null;
    onSelect: (id: string) => void;
}

const STATUS_CLASS: Record<string, string> = {
    Pending: 'status-pending',
    Running: 'status-running',
    Stopped: 'status-stopped',
    Completed: 'status-completed',
    Errored: 'status-errored',
};

// Full-width qBittorrent-style torrent table. Single selection; the row's
// Start/Stop/Remove actions live in the toolbar, not per row.
export default function SessionTable({sessions, selectedId, onSelect}: Props) {
    const header = (
        <thead>
        <tr>
            <th>Name</th>
            <th>Size</th>
            <th>Progress</th>
            <th>Status</th>
            <th>Seeds</th>
            <th>Peers</th>
            <th>Down</th>
            <th>Up</th>
            <th>ETA</th>
            <th>Ratio</th>
        </tr>
        </thead>
    );

    return (
        <table className="session-table">
            {header}
            <tbody>
            {sessions.length === 0 && (
                <tr>
                    <td colSpan={10} className="empty-hint">
                        No torrents yet — click Add to open a .torrent file.
                    </td>
                </tr>
            )}
            {sessions.map((s) => {
                const pct = s.TotalPieces > 0 ? Math.round((100 * s.DonePieces) / s.TotalPieces) : 0;
                return (
                    <tr
                        key={s.ID}
                        className={s.ID === selectedId ? 'selected' : ''}
                        onClick={() => onSelect(s.ID)}
                    >
                        <td className="session-name">{s.Name}</td>
                        <td>{formatBytes(s.TotalBytes)}</td>
                        <td className="progress-cell">
                            <div className="progress-track">
                                <div className="progress-fill" style={{width: `${pct}%`}}/>
                            </div>
                            <span className="progress-label">{pct}%</span>
                        </td>
                        <td>
                            <span className={`status-text ${STATUS_CLASS[s.Status] ?? ''}`}>{s.Status}</span>
                        </td>
                        <td>{formatSwarm(s.Seeds, s.TrackerSeeds)}</td>
                        <td>{formatSwarm(s.ActivePeers - s.Seeds, s.TrackerLeechers)}</td>
                        <td>{formatRate(s.DownloadRate)}</td>
                        <td>{formatRate(s.UploadRate)}</td>
                        <td>{formatEta(s)}</td>
                        <td>{formatRatio(s.TotalUploaded, s.TotalDownloaded)}</td>
                    </tr>
                );
            })}
            </tbody>
        </table>
    );
}
