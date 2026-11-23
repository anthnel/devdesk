# Recherche — process-list (2026-11-22)

Source : agent `product-team:researcher`, sources consultées le 2026-11-22. Étiquettes : [S] établi par une source, [I] inférence, [ND] aucune donnée trouvée. Condensé par l'orchestrateur ; les URL sont en fin de fichier.

## Synthèse

1. Les métriques (CPU, mémoire) sont le cœur de « remplacer le gestionnaire de tâches » ; elles sont faisables avec gopsutil, mais le CPU % exige deux échantillons. [I]
2. Le marché est saturé en lecture, pas en intégration : tous les outils font tri, recherche et arrêt ; seul bottom couvre Linux, macOS et Windows en TUI avec signaux et arbre. L'apport d'un écran DevDesk serait le contexte (lien avec Ports, ses propres port-forwards et scans, conteneurs), non démontré par une source. [I]
3. L'arrêt « propre » n'est pas symétrique : SIGTERM sous Unix ; sous Windows, gopsutil ne fait que `TerminateProcess`. [S]
4. gopsutil ne vérifie pas l'identité du processus avant de signaler : risque de PID réutilisé réel. [S]

## Alternatives

| Outil | Plateformes | Établi | Limites |
|---|---|---|---|
| htop | Linux, BSD, macOS, Solaris ; pas Windows [S] | visionneuse interactive | kill, arbre, recherche non confirmés par la page lue |
| btop | Linux, macOS, BSD ; pas de Windows natif [S] | signal au choix, arbre, filtre | macOS : suid root recommandé pour les processus des autres |
| bottom | Linux, macOS, Windows [S] | tri, recherche, signaux, arbre | Windows : `TerminateProcess(handle, 1)`, pas d'arrêt propre [S] |
| procs | Linux, Windows ; macOS « experimental » [S] | recherche multi-colonnes, ports, conteneur Docker, arbre, `watch` | kill non mentionné ; macOS : rien sur les processus des autres sans sudo |
| glances | Linux, macOS, FreeBSD, Windows [S] | liste filtrable, conteneurs, API REST, serveur MCP | kill non confirmé |
| gotop | Linux, FreeBSD, macOS ; Windows « limited testing » [S] | liste de processus | kill et tri non confirmés |
| Process Explorer | Windows [S] | handles, DLL, comptes | GUI Windows seule |
| Gestionnaire des tâches Windows | Windows | [ND] page non consultée | |
| Activity Monitor | macOS [S] | « Quit » (peut être refusé) / « Force Quit » | un processus d'autrui peut demander une authentification admin |
| taskkill | Windows [S] | `/f` force, `/t` arbre | sans `/f`, `WM_CLOSE` aux fenêtres du PID (winquit) |
| ntop, « k9s-like » pour processus | [ND] | | non identifiés |

## Preuves du problème

- Pour : serveurs de dev orphelins (`EADDRINUSE`, geste standard `lsof -i :PORT` puis `kill`, SIGTERM avant `kill -9`) ; `kubectl port-forward` oubliés (`pkill -f`, issue « Orphaned kubectl.exe processes on Windows »). [S, articles communautaires] L'onglet Ports de DevDesk les couvre déjà quand le processus tient un socket (`internal/ports/ports.go`). [S]
- Contre ou non démontré : aucune donnée sur la fréquence du besoin ni sur les utilisateurs de DevDesk qui quittent le TUI pour un gestionnaire de tâches ; aucune preuve que les processus sans socket qui consomment soient un problème fréquent. [ND]

## Faits techniques (gopsutil v4.26.7, version de `go.mod`)

- Linux : PID via `/proc`, ligne de commande `/proc/<pid>/cmdline`, mémoire `statm`, CPU `stat`, UID `status` ; `Processes()` saute les processus qui échouent. [S]
- macOS : `sysctl kern.proc.all`, `kern.procargs2`, `proc_pidinfo` (échecs ignorés pour mémoire et CPU) ; `Cwd` renvoie `ErrorNotPermitted` pour les processus inaccessibles ; sans sudo, procs ne donne rien sur les processus des autres. [S]
- Windows : PID via `EnumProcesses` ; CPU, mémoire, utilisateur via `OpenProcess` (`PROCESS_QUERY_LIMITED_INFORMATION`) ; ligne de commande via le PEB, chaîne vide sans erreur si accès refusé ; `SendSignal` renvoie `ErrNotImplementedError`, `Terminate`/`Kill` = `TerminateProcess`, aucun arrêt propre. [S]
- Dépôt : 294 processus nommés en 11 ms sous Windows par snapshot Toolhelp32 (commentaire de `ports.go`) ; la lecture par PID donne « Access denied » pour 98 sockets sur 189 sans élévation. [S, dépôt]
- CPU % : `Percent(0)` renvoie 0 au premier appel, puis le delta CPU / temps écoulé × 100 (pas ramené à 100 %) ; `CPUPercent()` est la moyenne sur la vie du processus. Il faut garder les objets `Process` entre deux rafraîchissements. [S, I]
- Coût d'une énumération complète avec métriques : aucun benchmark trouvé ; linéaire en nombre de processus. [ND, I]
- Arrêt propre sous Windows : `os.Process.Signal(Interrupt)` non implémenté en Go ; `CTRL_C_EVENT` ne vise pas un groupe précis ; `CTRL_BREAK_EVENT` ne marche que pour un groupe créé par `CREATE_NEW_PROCESS_GROUP` partageant la console ; `WM_CLOSE` sur les fenêtres (ce que fait taskkill sans `/f`, et winquit) : ignoré par une application console ou bloquée, limité à la session. [S] Conclusion : un processus arbitraire n'est atteignable que par `WM_CLOSE` (applications à fenêtre) puis `TerminateProcess` ; un enfant lancé par DevDesk peut recevoir `CTRL_BREAK`. [I]
- macOS : SIP refuse `task_for_pid` même en root (la page Apple ne dit pas si SIP restreint les signaux [ND]) ; `kill(2)` suit POSIX (EPERM si l'UID ne correspond pas). [S]
- Linux `hidepid` : `=1` protège `cmdline`/`status` des autres, `=2` cache aussi leurs répertoires ; la liste est incomplète sans erreur. [S, I]

## Risques d'un outil qui tue des processus

- PID réutilisé : gopsutil fait `os.FindProcess` puis `Signal` sans vérifier l'identité ; atténuation : comparer `CreateTime` juste avant l'envoi, et sous Linux ≥ 5.3 `pidfd` (`os.Process.WithHandle` l'expose sur Linux ≥ 5.4 et Windows). [S, I]
- Se tuer soi-même : aucune source ; conséquence évidente de signaler n'importe quel PID. Sous Windows, `GenerateConsoleCtrlEvent` avec 0 atteint la console de DevDesk. [I, S]
- Processus critiques : PID 1 ne reçoit que les signaux qu'il gère ; certains processus Windows protégés refusent même un administrateur (non vérifié par une source). [S, ND]
- Enfants : `Process.Kill` ne tue pas les enfants ; `taskkill /t` tue l'arbre. [S]
- Zombies : un zombie ne disparaît pas avant d'être attendu, `kill` sans effet visible. [S]

## Lacunes

Fréquence réelle du besoin ; coût mesuré CPU/mémoire/ligne de commande par processus sur les trois OS ; comportement de `Percent(0)` sous Windows sur les processus protégés ; Gestionnaire des tâches Windows non consulté ; kill de glances et gotop ; SIP et signaux ; WSL1/WSL2.

## Sources (consultées le 2026-11-22)

gopsutil process (pkg.go.dev, v4.26.7 : `process_windows.go`, `process.go`, `process_darwin.go`, `process_linux.go`) ; Go `os` Process et issues 6720, 7479 ; MS Learn : `GenerateConsoleCtrlEvent`, `taskkill`, Process Explorer ; containers/winquit ; man7 `proc(5)`, `kill(2)`, `pidfd_open(2)` ; Apple : SIP runtime protections, Activity Monitor ; READMEs de bottom, btop, procs, glances, gotop ; htop.dev ; articles `EADDRINUSE` (dev.to, betterstack, raycast) et port-forwards orphelins (markaicode, lightrun/vscode-kubernetes-tools).
