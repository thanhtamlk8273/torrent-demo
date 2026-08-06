import {peermgr} from '../../wailsjs/go/models';
import {formatBytes} from '../format';

export default function PeerTable({peers}: { peers: peermgr.PeerSnapshot[] }) {
    if (peers.length === 0) {
        return <div className="empty-hint">No peers discovered yet.</div>;
    }
    return (
        <table className="peer-table">
            <thead>
            <tr>
                <th>Status</th>
                <th>Address</th>
                <th>Downloaded</th>
                <th>Uploaded</th>
                <th>Have</th>
            </tr>
            </thead>
            <tbody>
            {peers.map((p) => (
                <tr key={p.Tag}>
                    <td>{p.Active ? 'active' : 'waiting'}</td>
                    <td className="mono">{p.Address}</td>
                    <td>{formatBytes(p.DownloadedBytes)}</td>
                    <td>{formatBytes(p.UploadedBytes)}</td>
                    <td>{Math.round(p.BitfieldFraction * 100)}%</td>
                </tr>
            ))}
            </tbody>
        </table>
    );
}
