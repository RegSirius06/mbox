package io.mbox.android;

import android.app.PendingIntent;
import android.content.ComponentName;
import android.content.Context;
import android.content.Intent;
import android.graphics.drawable.Icon;
import android.net.VpnService;
import android.os.Build;
import android.service.quicksettings.Tile;
import android.service.quicksettings.TileService;
import android.util.Log;

/** User-triggered VPN switch. No polling, background timer or second VPN engine. */
public final class MBoxTileService extends TileService {
    static final String ACTION_CONNECT_FROM_TILE = "io.mbox.android.CONNECT_FROM_TILE";
    private static final String TAG = "MBox";

    @Override public void onStartListening() {
        super.onStartListening();
        refresh();
    }

    @Override public void onClick() {
        super.onClick();
        // Always use the existing VPN service and its fail-closed supervisor.
        if (MBoxVpnService.getState() != MBoxVpnService.State.STOPPED) {
            startService(new Intent(this, MBoxVpnService.class).setAction(MBoxVpnService.ACTION_STOP));
            return;
        }
        if (!ProfileStore.hasProfile(this) || VpnService.prepare(this) != null) {
            openForConsent();
            return;
        }
        try {
            startForegroundService(new Intent(this, MBoxVpnService.class)
                    .setAction(MBoxVpnService.ACTION_START));
        } catch (RuntimeException denied) {
            Log.w(TAG, "QS tile: direct VPN start unavailable", denied);
            openForConsent();
        }
    }

    private void openForConsent() {
        Intent intent = new Intent(this, MainActivity.class)
                .setAction(ACTION_CONNECT_FROM_TILE)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TOP
                        | Intent.FLAG_ACTIVITY_SINGLE_TOP);
        if (Build.VERSION.SDK_INT >= 34) {
            PendingIntent pending = PendingIntent.getActivity(this, 84, intent,
                    PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
            startActivityAndCollapse(pending);
        } else {
            startActivityAndCollapse(intent);
        }
    }

    private void refresh() {
        Tile tile = getQsTile();
        if (tile == null) return;
        MBoxVpnService.State state = MBoxVpnService.getState();
        tile.setState(state == MBoxVpnService.State.STOPPED ? Tile.STATE_INACTIVE : Tile.STATE_ACTIVE);
        tile.setLabel("MBox VPN");
        tile.setIcon(Icon.createWithResource(this, R.drawable.ic_mbox_tile));
        tile.updateTile();
    }

    static void requestRefresh(Context context) {
        try {
            TileService.requestListeningState(context,
                    new ComponentName(context, MBoxTileService.class));
        } catch (RuntimeException unavailable) {
            // Devices without the tile (or a QS implementation) must still run VPN.
            Log.d(TAG, "QS tile state refresh unavailable");
        }
    }
}
