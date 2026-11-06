package io.mbox.android;

import android.content.Context;
import android.net.Uri;
import android.util.Base64;
import org.json.JSONObject;
import java.io.File;
import java.io.FileOutputStream;
import java.net.URI;
import java.net.URLDecoder;
import java.nio.charset.StandardCharsets;
import java.nio.file.AtomicMoveNotSupportedException;
import java.nio.file.Files;
import java.nio.file.StandardCopyOption;
import java.security.MessageDigest;
import java.security.SecureRandom;
import java.util.Locale;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

public final class ProfileStore {
    // Profile links may originate from untrusted Android VIEW intents or clipboard.
    // Limit base64/JSON size before expensive parsing or profile confirmation UI.
    private static final int MAX_LINK_CHARS = 64 * 1024;
    private static final int MAX_PROFILE_BYTES = 48 * 1024;
    private static final String PREFS="mbox-profile";
    private static final String DEFAULT_ANDROID_HTTP_STACK="cronet";
    private static final String PREF_NAME="name";
    private static final String PREF_DEVICE_ID="device_id";
    private static final SecureRandom RANDOM=new SecureRandom();
    public static final class Candidate {
        public final String name,clientId,endpoint,psk,serverPublicKey,serverFingerprint,androidHttpStack;
        private final JSONObject camouflage,diagnostics;
        private final List<String> securityWarnings;
        Candidate(String n,String c,String e,String p,String k,String f,String hs,JSONObject cam,JSONObject diag,List<String> warnings){name=n;clientId=c;endpoint=e;psk=p;serverPublicKey=k;serverFingerprint=f;androidHttpStack=hs;camouflage=cam;diagnostics=diag;securityWarnings=Collections.unmodifiableList(new ArrayList<>(warnings));}
        public boolean requiresDangerousConsent(){return !securityWarnings.isEmpty();}
        public String securityWarningText(){return String.join("\n",securityWarnings);}
    }
    public static final class RuntimeMaterialization implements AutoCloseable {
        public final File configFile;
        private final File directory;
        RuntimeMaterialization(File config,File dir){configFile=config;directory=dir;}
        @Override public void close(){deleteTree(directory);}
    }
    public static File profileDir(Context c){return new File(c.getFilesDir(),"profile");}
    public static File configFile(Context c){return new File(profileDir(c),"client.json");}
    public static boolean hasProfile(Context c){return configFile(c).isFile();}
    public static synchronized String installationDeviceId(Context c){String existing=c.getSharedPreferences(PREFS,Context.MODE_PRIVATE).getString(PREF_DEVICE_ID,"");if(existing!=null&&existing.matches("[0-9a-f]{32}"))return existing;byte[] b=new byte[16];RANDOM.nextBytes(b);StringBuilder out=new StringBuilder(32);for(byte x:b)out.append(String.format(Locale.ROOT,"%02x",x&0xff));String id=out.toString();c.getSharedPreferences(PREFS,Context.MODE_PRIVATE).edit().putString(PREF_DEVICE_ID,id).commit();return id;}
    public static String profileName(Context c){return c.getSharedPreferences(PREFS,Context.MODE_PRIVATE).getString(PREF_NAME,"MBox");}
    private static void writePrivateFile(File d,String text)throws Exception{File p=d.getParentFile();if(p==null||(!p.isDirectory()&&!p.mkdirs()))throw new Exception("Не удалось создать каталог профиля");try(FileOutputStream out=new FileOutputStream(d,false)){out.write(text.getBytes(StandardCharsets.UTF_8));out.getFD().sync();}d.setReadable(false,false);d.setWritable(false,false);d.setReadable(true,true);d.setWritable(true,true);}
    private static boolean isHex32(String v){return v!=null&&v.matches("[0-9a-fA-F]{64}");}
    private static boolean validClientId(String id){if(id==null||id.isEmpty()||id.length()>63)return false;for(int i=0;i<id.length();i++){char c=id.charAt(i);if(!(c>='a'&&c<='z')&&!(c>='A'&&c<='Z')&&!(c>='0'&&c<='9')&&c!='.'&&c!='_'&&c!='-')return false;}return true;}
    private static void validateBaseUrl(String raw)throws Exception{URI u=new URI(raw);String scheme=u.getScheme(),host=u.getHost();if(scheme==null||host==null||u.getRawUserInfo()!=null||u.getRawQuery()!=null||u.getRawFragment()!=null||raw.endsWith("/"))throw new Exception("Некорректный transport.url");if("https".equalsIgnoreCase(scheme))return;boolean loop="127.0.0.1".equals(host)||"::1".equals(host)||"localhost".equalsIgnoreCase(host);if(!"http".equalsIgnoreCase(scheme)||!loop)throw new Exception("Удалённый MBox endpoint должен использовать HTTPS");}
    private static byte[] decodeHex(String h)throws Exception{if((h.length()&1)!=0)throw new Exception("Некорректный hex");byte[] o=new byte[h.length()/2];for(int i=0;i<o.length;i++){int hi=Character.digit(h.charAt(i*2),16),lo=Character.digit(h.charAt(i*2+1),16);if(hi<0||lo<0)throw new Exception("Некорректный hex");o[i]=(byte)((hi<<4)|lo);}return o;}
    private static String fingerprint(String k)throws Exception{byte[] d=MessageDigest.getInstance("SHA-256").digest(decodeHex(k));StringBuilder o=new StringBuilder();for(int i=0;i<d.length;i++){if(i>0&&(i%4)==0)o.append(' ');o.append(String.format(Locale.ROOT,"%02x",d[i]&0xff));}return o.toString();}
    public static String extractProvisioningLink(String input)throws Exception{if(input!=null&&input.length()>MAX_LINK_CHARS)throw new Exception("MBox link слишком длинный");String v=input==null?"":input.trim();if(v.regionMatches(true,0,"mbox://",0,7))return v;Uri u=Uri.parse(v);if("https".equalsIgnoreCase(u.getScheme())){if(u.getQueryParameter("profile")!=null)throw new Exception("Небезопасный legacy provisioning через URL query больше не поддерживается");String fragment=u.getEncodedFragment();if(fragment!=null&&!fragment.isEmpty()){Uri f=Uri.parse("https://fragment.invalid/?"+fragment);String nested=f.getQueryParameter("profile");if(nested!=null&&nested.regionMatches(true,0,"mbox://",0,7))return nested;}}throw new Exception("Это не MBox provisioning link");}
        public static Candidate parseLink(String link)throws Exception{String input=extractProvisioningLink(link);String payload=input.substring(7),fragment="";int hash=payload.indexOf('#');if(hash>=0){fragment=payload.substring(hash+1);payload=payload.substring(0,hash);}payload=payload.replaceAll("\\s+","");if(payload.length()>MAX_LINK_CHARS)throw new Exception("MBox profile слишком большой");byte[] decoded=Base64.decode(payload,Base64.URL_SAFE|Base64.NO_WRAP|Base64.NO_PADDING);if(decoded.length>MAX_PROFILE_BYTES)throw new Exception("MBox profile слишком большой");JSONObject root=new JSONObject(new String(decoded,StandardCharsets.UTF_8));if(root.getInt("v")!=1)throw new Exception("Неподдерживаемая версия MBox link");String id=root.optString("client_id","");if(!validClientId(id))throw new Exception("Некорректный client_id");JSONObject tr=root.getJSONObject("transport"),cr=root.getJSONObject("crypto"),cam=root.getJSONObject("camouflage");String endpoint=tr.getString("url"),httpStack=normalizeHttpStack(tr.optString("android_http_stack",DEFAULT_ANDROID_HTTP_STACK)),psk=cr.getString("psk").toLowerCase(Locale.ROOT),pub=cr.getString("server_public_key").toLowerCase(Locale.ROOT);validateBaseUrl(endpoint);if(!isHex32(psk))throw new Exception("Некорректный PSK");if(!isHex32(pub))throw new Exception("Некорректный server public key");String name=root.optString("name","");if(name.isEmpty()&&!fragment.isEmpty())name=URLDecoder.decode(fragment,StandardCharsets.UTF_8.name());if(name.isEmpty())name=id;JSONObject diag=root.optJSONObject("diagnostics");if(diag==null)diag=new JSONObject();List<String>warnings=new ArrayList<>();if("go".equals(httpStack))warnings.add("android_http_stack=go: диагностический Go HTTPS fingerprint");if(diag.optBoolean("network_debug",false))warnings.add("network_debug=true: расширенное сетевое логирование");if(diag.optBoolean("deterministic_transport",false))warnings.add("deterministic_transport=true: воспроизводимый traffic pattern");return new Candidate(name,id,endpoint,psk,pub,fingerprint(pub),httpStack,new JSONObject(cam.toString()),new JSONObject(diag.toString()),warnings);}
    private static String normalizeHttpStack(String raw)throws Exception{String v=raw==null?DEFAULT_ANDROID_HTTP_STACK:raw.trim().toLowerCase(Locale.ROOT);if(v.isEmpty())v=DEFAULT_ANDROID_HTTP_STACK;if(!"cronet".equals(v)&&!"go".equals(v))throw new Exception("android_http_stack должен быть cronet или go");return v;}
    private static String generationName(){byte[] b=new byte[8];RANDOM.nextBytes(b);StringBuilder s=new StringBuilder("gen-");for(byte x:b)s.append(String.format(Locale.ROOT,"%02x",x&0xff));return s.toString();}
    public static String commitCandidate(Context c,Candidate x)throws Exception{File root=profileDir(c);if(!root.isDirectory()&&!root.mkdirs())throw new Exception("Не удалось создать каталог профиля");File gen=new File(root,generationName());if(!gen.mkdir())throw new Exception("Не удалось создать поколение профиля");boolean committed=false;try{File pskEnc=new File(gen,"mbox.psk.enc"),pub=new File(gen,"mbox-server.pub");CredentialVault.encryptToFile(pskEnc,x.psk+"\n");writePrivateFile(pub,x.serverPublicKey+"\n");JSONObject dc=new JSONObject();dc.put("client_id",x.clientId);dc.put("psk_encrypted_file",pskEnc.getAbsolutePath());dc.put("server_public_key_file",pub.getAbsolutePath());JSONObject dt=new JSONObject();dt.put("listen",AppRuntime.socksListen());dt.put("url",x.endpoint);dt.put("android_http_stack",x.androidHttpStack);JSONObject disk=new JSONObject();disk.put("camouflage",x.camouflage);disk.put("transport",dt);disk.put("crypto",dc);disk.put("diagnostics",x.diagnostics);File pending=new File(root,"client.json.next");writePrivateFile(pending,disk.toString(2)+"\n");try{Files.move(pending.toPath(),configFile(c).toPath(),StandardCopyOption.ATOMIC_MOVE,StandardCopyOption.REPLACE_EXISTING);}catch(AtomicMoveNotSupportedException e){throw new Exception("Файловая система не поддерживает атомарную замену профиля",e);}committed=true;c.getSharedPreferences(PREFS,Context.MODE_PRIVATE).edit().putString(PREF_NAME,x.name).commit();cleanupOldGenerations(root,gen);new File(root,"mbox.psk").delete();new File(root,"mbox-server.pub").delete();return x.name;}finally{if(!committed){deleteTree(gen);new File(root,"client.json.next").delete();}}}
    public static synchronized RuntimeMaterialization materializeRuntimeConfig(Context c)throws Exception{cleanupRuntimeMaterializations(c);File committed=configFile(c);JSONObject root=new JSONObject(new String(Files.readAllBytes(committed.toPath()),StandardCharsets.UTF_8));JSONObject crypto=root.getJSONObject("crypto");File encrypted;String legacy=crypto.optString("psk_file","");String enc=crypto.optString("psk_encrypted_file","");if(enc.isEmpty()){if(legacy.isEmpty())throw new Exception("Профиль не содержит credential");File legacyFile=new File(legacy);String plain=new String(Files.readAllBytes(legacyFile.toPath()),StandardCharsets.UTF_8);File parent=legacyFile.getParentFile();encrypted=new File(parent,"mbox.psk.enc");CredentialVault.encryptToFile(encrypted,plain);crypto.remove("psk_file");crypto.put("psk_encrypted_file",encrypted.getAbsolutePath());File pending=new File(profileDir(c),"client.json.migrate");writePrivateFile(pending,root.toString(2)+"\n");try{Files.move(pending.toPath(),committed.toPath(),StandardCopyOption.ATOMIC_MOVE,StandardCopyOption.REPLACE_EXISTING);}catch(AtomicMoveNotSupportedException e){pending.delete();throw new Exception("Не удалось атомарно мигрировать PSK в Android Keystore",e);}legacyFile.delete();}else{encrypted=new File(enc);}String psk=CredentialVault.decryptFromFile(encrypted);if(!isHex32(psk.trim()))throw new Exception("Расшифрованный PSK повреждён");File dir=new File(c.getCacheDir(),generationName()+"-runtime");if(!dir.mkdir())throw new Exception("Не удалось создать временный runtime-каталог");boolean ok=false;try{File plainFile=new File(dir,"mbox.psk");writePrivateFile(plainFile,psk.trim()+"\n");JSONObject runtime=new JSONObject(root.toString());runtime.getJSONObject("transport").put("enable_udp_lab",AppRuntime.udpLabEnabled(c));JSONObject runtimeCrypto=runtime.getJSONObject("crypto");runtimeCrypto.remove("psk_encrypted_file");runtimeCrypto.put("psk_file",plainFile.getAbsolutePath());File runtimeConfig=new File(dir,"client.json");writePrivateFile(runtimeConfig,runtime.toString(2)+"\n");ok=true;return new RuntimeMaterialization(runtimeConfig,dir);}finally{if(!ok)deleteTree(dir);}}
    private static void cleanupOldGenerations(File root,File keep){File[] es=root.listFiles();if(es==null)return;for(File e:es)if(e.isDirectory()&&e.getName().startsWith("gen-")&&!e.equals(keep))deleteTree(e);}
    private static void cleanupRuntimeMaterializations(Context c){File[] es=c.getCacheDir().listFiles();if(es==null)return;for(File e:es)if(e.isDirectory()&&e.getName().startsWith("gen-")&&e.getName().endsWith("-runtime"))deleteTree(e);}
    private static void deleteTree(File f){if(f==null||!f.exists())return;if(f.isDirectory()){File[] cs=f.listFiles();if(cs!=null)for(File c:cs)deleteTree(c);}f.delete();}
    public static String transportUrl(Context c)throws Exception{byte[] data=Files.readAllBytes(configFile(c).toPath());JSONObject root=new JSONObject(new String(data,StandardCharsets.UTF_8));return root.getJSONObject("transport").getString("url");}
    public static String transportHost(Context c)throws Exception{URI u=new URI(transportUrl(c));String h=u.getHost();if(h==null||h.isEmpty())throw new Exception("В профиле отсутствует hostname MBox endpoint");return h;}
    public static String androidHttpStack(Context c){try{byte[] data=Files.readAllBytes(configFile(c).toPath());JSONObject root=new JSONObject(new String(data,StandardCharsets.UTF_8));return normalizeHttpStack(root.getJSONObject("transport").optString("android_http_stack",DEFAULT_ANDROID_HTTP_STACK));}catch(Exception ignored){return DEFAULT_ANDROID_HTTP_STACK;}}
    public static boolean networkDebug(Context c){try{byte[] data=Files.readAllBytes(configFile(c).toPath());JSONObject root=new JSONObject(new String(data,StandardCharsets.UTF_8));JSONObject d=root.optJSONObject("diagnostics");return d!=null&&d.optBoolean("network_debug",false);}catch(Exception ignored){return false;}}
    public static String importLink(Context c,String link)throws Exception{return commitCandidate(c,parseLink(link));}
    private ProfileStore(){}
}
