package io.mbox.android;

import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyProperties;

import java.io.ByteArrayOutputStream;
import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.nio.charset.StandardCharsets;
import java.security.KeyStore;
import java.util.Arrays;

import javax.crypto.Cipher;
import javax.crypto.KeyGenerator;
import javax.crypto.SecretKey;
import javax.crypto.spec.GCMParameterSpec;

/** Encrypts reusable profile credentials with a non-exportable Android Keystore key. */
final class CredentialVault {
    private static final String STORE = "AndroidKeyStore";
    private static final String ALIAS = "mbox-profile-secrets-v1";
    private static final byte[] MAGIC = new byte[]{'M','B','X','1'};

    private CredentialVault() {}

    private static SecretKey key() throws Exception {
        KeyStore store = KeyStore.getInstance(STORE);
        store.load(null);
        java.security.Key existing = store.getKey(ALIAS, null);
        if (existing instanceof SecretKey) return (SecretKey) existing;
        KeyGenerator generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, STORE);
        generator.init(new KeyGenParameterSpec.Builder(ALIAS,
                KeyProperties.PURPOSE_ENCRYPT | KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build());
        return generator.generateKey();
    }

    static void encryptToFile(File destination, String plaintext) throws Exception {
        Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
        cipher.init(Cipher.ENCRYPT_MODE, key());

        // Android Keystore generates a fresh IV for each encryption.
        byte[] iv = cipher.getIV();
        if (iv == null || iv.length != 12) {
            throw new IllegalStateException("Invalid AES-GCM IV length");
        }

        byte[] encrypted = cipher.doFinal(plaintext.getBytes(StandardCharsets.UTF_8));
        File parent = destination.getParentFile();
        if (parent == null || (!parent.isDirectory() && !parent.mkdirs())) throw new Exception("Не удалось создать каталог секрета");
        try (FileOutputStream out = new FileOutputStream(destination, false)) {
            out.write(MAGIC);
            out.write(iv.length);
            out.write(iv);
            out.write(encrypted);
            out.getFD().sync();
        }
        destination.setReadable(false, false);
        destination.setWritable(false, false);
        destination.setReadable(true, true);
        destination.setWritable(true, true);
        Arrays.fill(encrypted, (byte) 0);
    }

    static String decryptFromFile(File source) throws Exception {
        byte[] raw;
        try (FileInputStream in = new FileInputStream(source); ByteArrayOutputStream out = new ByteArrayOutputStream()) {
            byte[] buf = new byte[4096];
            int n;
            while ((n = in.read(buf)) >= 0) {
                if (out.size() + n > 64 * 1024) throw new Exception("Зашифрованный credential слишком большой");
                out.write(buf, 0, n);
            }
            raw = out.toByteArray();
        }
        if (raw.length < MAGIC.length + 1 + 12 + 16) throw new Exception("Повреждён encrypted credential");
        for (int i = 0; i < MAGIC.length; i++) if (raw[i] != MAGIC[i]) throw new Exception("Неизвестный формат encrypted credential");
        int ivLen = raw[MAGIC.length] & 0xff;
        if (ivLen != 12 || raw.length < MAGIC.length + 1 + ivLen + 16) throw new Exception("Повреждён IV encrypted credential");
        int off = MAGIC.length + 1;
        byte[] iv = Arrays.copyOfRange(raw, off, off + ivLen);
        byte[] ciphertext = Arrays.copyOfRange(raw, off + ivLen, raw.length);
        Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
        cipher.init(Cipher.DECRYPT_MODE, key(), new GCMParameterSpec(128, iv));
        byte[] plain = cipher.doFinal(ciphertext);
        try {
            return new String(plain, StandardCharsets.UTF_8);
        } finally {
            Arrays.fill(plain, (byte) 0);
            Arrays.fill(ciphertext, (byte) 0);
            Arrays.fill(raw, (byte) 0);
        }
    }
}
