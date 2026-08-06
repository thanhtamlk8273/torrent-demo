import React from 'react';
import {session} from '../../wailsjs/go/models';
import {
    formatBytes,
    formatDate,
    formatEta,
    formatRate,
    formatRatio,
    formatSwarm,
    formatTimeActive,
} from '../format';

interface Props {
    summary: session.SessionSummary;
}

// One labeled grid cell. wide spans the full grid row (save path, info hash,
// comment, error).
function Field({label, value, wide, wrap, mono}: {
    label: string;
    value: React.ReactNode;
    wide?: boolean;
    wrap?: boolean;
    mono?: boolean;
}) {
    return (
        <div className={'field' + (wide ? ' wide' : '')}>
            <span className="field-label">{label}</span>
            <span className={'field-value' + (wrap ? ' wrap' : '') + (mono ? ' mono' : '')}>{value}</span>
        </div>
    );
}

// Purely presentational: every field arrives on the existing 1s sessions:update
// push, so no bound method and no polling are needed here.
export default function GeneralTab({summary: s}: Props) {
    const pct = s.TotalPieces > 0 ? Math.round((100 * s.DonePieces) / s.TotalPieces) : 0;

    return (
        <div className="general-tab">
            <section className="info-section">
                <h3 className="section-title">Transfer</h3>
                <div className="general-grid">
                    <Field label="Time active" value={formatTimeActive(s)}/>
                    <Field label="ETA" value={formatEta(s)}/>
                    <Field label="Status" value={s.Status}/>
                    <Field label="Progress" value={`${pct}%`}/>
                    <Field label="Connections" value={`${s.ActivePeers} (${s.MaxConnections} max)`}/>
                    <Field label="Seeds" value={formatSwarm(s.Seeds, s.TrackerSeeds)}/>
                    <Field label="Leechers" value={formatSwarm(s.ActivePeers - s.Seeds, s.TrackerLeechers)}/>
                    <Field label="Queued peers" value={s.WaitingPeers}/>
                    <Field label="Downloaded" value={formatBytes(s.TotalDownloaded)}/>
                    <Field label="Uploaded" value={formatBytes(s.TotalUploaded)}/>
                    <Field label="Down speed" value={formatRate(s.DownloadRate)}/>
                    <Field label="Up speed" value={formatRate(s.UploadRate)}/>
                    <Field label="Share ratio" value={formatRatio(s.TotalUploaded, s.TotalDownloaded)}/>
                </div>
            </section>

            <section className="info-section">
                <h3 className="section-title">Information</h3>
                <div className="general-grid">
                    <Field label="Save path" value={s.SavePath} wide wrap/>
                    <Field label="Total size" value={formatBytes(s.TotalBytes)}/>
                    <Field label="Pieces" value={`${s.TotalPieces} × ${formatBytes(s.PieceLength)}, have ${s.DonePieces}`}/>
                    <Field label="Info hash v1" value={s.InfoHash} wide wrap mono/>
                    <Field label="Added on" value={formatDate(s.AddedAt)}/>
                    <Field label="Started on" value={formatDate(s.StartedAt)}/>
                    <Field label="Completed on" value={formatDate(s.CompletedAt)}/>
                    <Field label="Created on" value={formatDate(s.CreationDate)}/>
                    <Field label="Created by" value={s.CreatedBy}/>
                    <Field label="Privacy" value={s.Private ? 'Private torrent' : 'Public torrent'}/>
                    <Field label="Comment" value={s.Comment} wide wrap/>
                    {s.Error && (
                        <Field label="Error" value={s.Error} wide wrap/>
                    )}
                </div>
            </section>
        </div>
    );
}
