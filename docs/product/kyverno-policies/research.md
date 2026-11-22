# Recherche — kyverno-policies

Date de consultation de toutes les sources : 2026-11-22. Méthode : pages web et code source lus via un lecteur qui résume (donc lossy) ; rien n'a été exécuté. Aucun `kyverno apply` n'a été lancé, la forme exacte du JSON reste à mesurer.

## Question de recherche

Un étage optionnel « politiques Kyverno de l'organisation » est-il faisable avec `kyverno apply` sur des manifestes hors cluster, et quelles alternatives CLI existent pour des règles propres à l'organisation ?

## Synthèse

1. **Rupture de format dans Kyverno 1.20.0** (dernière version stable, publiée le 12 novembre ; année déduite de 2026) : le chargeur de `kyverno apply` rejette avec une erreur les politiques héritées `kyverno.io` (`ClusterPolicy`, `Policy`, `CleanupPolicy`, `PolicyException`...) au lieu de les lire. Seules les politiques `policies.kyverno.io` (ValidatingPolicy, MutatingPolicy, GeneratingPolicy, ImageValidatingPolicy, DeletingPolicy, plus VAP/MAP) sont chargées. Un répertoire d'organisation encore en `ClusterPolicy` ferait échouer l'étage. Le backlog §3.85 cite 1.19.1 : à mettre à jour.
2. **Pas de numéro de ligne** : le rapport est construit à partir de références d'objet (kind, name, namespace, UID, apiVersion), regroupé par politique, sans chemin de fichier ni ligne. Le lien ressource → fichier → ligne est à faire côté DevDesk (comme pour kubeconform).
3. **Rapport JSON** : `-p --output-format json` imprime un `ClusterReport` fusionné (cluster) et un `Report` par politique namespacée, au format `openreports` (`openreportsv1alpha1`), pas `wgpolicyk8s` ; plusieurs documents peuvent être émis à la suite (voir Constats).
4. **Codes de sortie** : 1 s'il y a des échecs (`Fail`) ou des erreurs ; `--warn-exit-code` pour les warnings. Un retour 0 malgré des violations a été rapporté en 1.12.5 (blog tiers). Ne pas se fier au seul code de sortie : lire le rapport.
5. **Alternatives** : Conftest (Rego) est la seule à annoncer des numéros de ligne (`_loc`, sur sortie JSON), mais impose Rego et le Rego n'est pas du Kyverno. Aucune alternative ne lit les politiques Kyverno. Datree est archivé (6 juin 2024). La demande réelle des équipes pour « Kyverno CLI en CI » est attestée par la doc officielle et des blogs ; pas de chiffre de fréquence trouvé.

## Tableau comparatif (politiques d'organisation sur manifestes hors cluster)

Légende : V = vérifié dans une source citée, ? = pas de donnée trouvée / non confirmé.

| Outil | Politiques de l'utilisateur | Sortie JSON | N° de ligne | Helm / Kustomize | Image officielle | Maintenance / licence |
|---|---|---|---|---|---|---|
| `kyverno apply` / `kyverno test` | V : politiques Kyverno (nouveaux types uniquement en 1.20) | V : `--policy-report --output-format json` ; schéma détaillé ? | V (code) : absent du rapport, références d'objet seulement | ? : pas de rendu (backlog §3.85) ; Kyverno n'en rend pas | V : `ghcr.io/kyverno/kyverno-cli` | V : Apache-2.0, CNCF graduated (24 mars 2026), 1.20.0 le 12 nov |
| Conftest (OPA/Rego) | V : Rego, depuis chemin, OCI ou Git | V : JSON, SARIF | V : champ optionnel `_loc` (fichier et ligne) dans JSON et GitHub ; la règle doit le renseigner | ? : la page d'accueil le liste, non vérifié ailleurs ; usage courant via `helm template \|` | V : `openpolicyagent/conftest` | V : Apache-2.0 ; dernière version ? (v0.68.x à v0.70.x selon sources) |
| Kubescape | V (partiel) : frameworks et contrôles Rego en JSON (regolibrary), source de 2024 ou avant ; mise en oeuvre hors ligne ? | V : JSON, SARIF, JUnit, HTML, PDF, CSV | ? | V : Helm (dépendances déjà vendorées), Kustomize (répertoire) | ? : aucune image de la CLI trouvée | V : Apache-2.0, CNCF incubating |
| Polaris | V : contrôles personnalisés en JSON Schema | ? (page CLI non lue) | ? | ? (non lu) | V : `us-docker.pkg.dev/fairwinds-ops/oss/polaris` (ancien quay.io déprécié) | licence ? ; pas d'avis d'archivage vu |
| kube-score | ? : aucune mention de contrôle personnalisé | V : `human`, `json` (v2), `ci`, `sarif` | ? | V : par pipe (`helm template`, `kustomize build`) | V (non qualifiée d'officielle) : `zegl/kube-score` | V : MIT ; statut ? |
| Checkov | V : politiques YAML ou Python | V : JSON (champs ? ; la CLI affiche des plages de lignes) | V (CLI) : plages `fichier:début-fin` ; dans le JSON non confirmé | V : Helm et Kustomize comme cibles | V : `bridgecrew/checkov` | V : Apache-2.0, Prisma Cloud |
| Datree | (historique) règles YAML | ? | ? | ? | ? | V : archivé le 6 juin 2024, société fermée depuis juillet 2023 |

Remarque : Kubescape, Polaris, kube-score et Checkov apportent leur propre catalogue ; seule la ligne Kyverno applique les politiques **déjà écrites** par l'utilisateur en format Kyverno, donc sans réécriture.

## Constats vérifiés vs supposés (Kyverno CLI)

| Sujet | Statut | Constat |
|---|---|---|
| Version courante | Vérifié | 1.20.0, « Latest » sur la page des versions GitHub. Le backlog écrit 1.19.1. |
| Flags du rapport | Vérifié (doc) | `-p/--policy-report`, `--output-format json\|yaml` (défaut yaml), `--audit-warn`, `--detailed-results`, `--warn-exit-code`, `--warn-no-pass`, `--warnings-as-errors`, `-r/--resource`, `--output-format` hors rapport accepte `table`. `--explain` refusé avec `--policy-report`. Format non reconnu : avertissement et repli sur yaml. |
| Structure du rapport | Vérifié (code `print.go`, `report.go`) | Résultats regroupés **par politique**. `ClusterReport` (fusionné, nommé « merged ») pour les politiques sans namespace ; un `Report` par politique namespacée, préfixé de `---` en yaml. Chaque résultat porte une référence d'objet (Kind, Name, Namespace, UID, APIVersion, ResourceVersion). En JSON, une sortie multi-rapports n'est pas un document unique : `json.Marshal` par rapport, un par ligne. À tester. |
| Champs `result`, `severity`, `rule`, `resources` | Partiellement vérifié | `result` : pass, skip, fail, warn, error (doc des rapports). `severity` et `category` existent dans les entrées de rapport de la doc 1.12 (exemple `severity: medium`). Le remplissage dans la sortie CLI se fait dans `reportutils.ToPolicyReportResult`, non lu. **Non vérifié** : noms exacts des champs en JSON CLI 1.20. |
| Annotation de sévérité | Vérifié (doc tierce, exemple) | `policies.kyverno.io/severity` apparaît sur une ValidatingPolicy d'exemple. Valeurs : low/medium/high selon une source, critical/high/medium/low selon une autre (Big Bang). **Non vérifié** : la valeur `critical` et le report de l'annotation d'une ValidatingPolicy dans le rapport CLI. Une absence de sévérité reste à tester. |
| `policies.kyverno.io/scored` | Vérifié (doc) | `"false"` transforme un `fail` en `warn`. |
| Codes de sortie | Vérifié (code) | Dans l'ordre : `Fail > 0` donne l'erreur « exit as there are policy violations » ; `Error > 0` donne « policy errors » ; `Warn > 0` avec `--warn-exit-code` donne ce code ; `Pass == 0` avec `--warn-no-pass` idem. La commande renvoie une erreur (pas de `os.Exit` dans ce fichier), le code final est posé par l'appelant. Un blog (avril, v1.12.5, année non indiquée) rapporte un 0 malgré des violations, attribué en partie à un pipe `tee`. |
| Politiques héritées (`ClusterPolicy`) | Vérifié (code, deux lectures concordantes, tag v1.20.0 et main) | Rejet avec erreur et conseil de migration, le fichier entier échoue. Une page de la doc de migration parle seulement d'un avertissement de dépréciation à propos de `apply`/`test` : ces deux sources se contredisent, le code l'emporte probablement mais **à confirmer en exécutant 1.20.0**. `--warnings-as-errors` fait échouer sur tout avertissement de dépréciation. |
| ValidatingPolicy (CEL) | Vérifié (code) | Chargée. Variantes Envoy et HTTP chargées aussi. Les VAP/MAP et leurs bindings sont chargées ; `--parameter-resource` fournit les paramètres. |
| `mutate` | Vérifié (code) | MutatingPolicy et MAP chargées ; les ressources mutées vont dans `--output-dir` ou `--output-file` ; la sortie par défaut ne rapporte que les règles ignorées ou en erreur. `-o/--output` déprécié et exclusif avec les deux. Cohérent avec la décision du brief (pas de correctif). |
| `generate` | Vérifié (code) | GeneratingPolicy chargée ; la sortie par défaut ne rapporte une règle `generate` que si son statut est erreur. Comportement hors cluster sur de vraies ressources générées : non lu. |
| `verifyImages` | Vérifié (code) | Pas de traitement dans `command.go` ; passe par `ImageValidatingPolicy` (`applyImageValidatingPolicies`). Comportement réseau (registre, signatures) hors cluster : **non vérifié**, risque d'accès réseau à traiter avec `internal/netproxy`. |
| Numéro de ligne | Vérifié (déduction du code lu) | Aucun champ de fichier ou de ligne dans les références d'objet. Aucune doc ne le promet. |
| Politique invalide / aucun manifeste | Non vérifié | Le chargeur ignore les fichiers non analysables (log niveau V(3)) ; `--continue-on-error` vaut true par défaut. Résultat sur répertoire vide : pas de donnée. |
| Rendu Helm / Kustomize | Non trouvé | La CLI ne rend pas ; backlog §3.85 le dit déjà. |
| Image conteneur | Vérifié | `ghcr.io/kyverno/kyverno-cli`, `latest` publiée récemment (tag court « il y a environ 13 h » à la lecture). Épinglage par version : tags non listés. |

## Preuves pour et contre l'existence du besoin

**Pour**
- Doc officielle Kyverno : exemple de workflow GitHub Actions (`kyverno apply policies/ -r resources/`, puis `kyverno test`), via l'action `kyverno-cli-installer` (doc 1.11). Dépôt `kyverno/pre-commit-hook` cité par un blog tiers. Blogs tiers (DEV Community, OneUptime 2026-01 et 2026-03, KCD Chennai) montrent l'usage en CI et en pre-commit. Ce sont des guides, pas des mesures d'usage.
- Kyverno est passé « graduated » à la CNCF le 24 mars 2026 (annonce CNCF, croissance de l'adoption alléguée, sans chiffre repris ici).
- Le chiffre le plus proche : une enquête CNCF ancienne (article Cloud Native Now, non daté dans le résultat) donne OPA à 31,6 % des répondants pour les politiques Kubernetes et 37,5 % sans outil de politique. Dépend d'un échantillon et d'une date inconnue.
- Des organisations migrent leurs ClusterPolicy vers les types CEL avant fin 2026 (épopées Big Bang, Nirmata) : preuve de parc de politiques existant.

**Contre / réserves**
- Pas de donnée trouvée sur la part des équipes qui appliquent des politiques d'organisation **à des dépôts sur poste développeur** plutôt qu'en CI ou en admission.
- Pas de chiffre 2025 de l'enquête CNCF sur la politique as code (le PDF n'a pas été lu).
- Un étage de scan DevDesk concurrence directement `kyverno apply` lancé à la main ou en CI ; le gain est l'intégration (onglet, filtres, rendu Helm partagé), non une capacité nouvelle.
- Rupture 1.20 : les politiques d'organisation encore en `ClusterPolicy` ne passent plus sans migration, donc une partie du public visé doit d'abord migrer.
- Fiabilité CI contestée : un blog rapporte des violations avec code 0 (1.12.5, ancienne version).

## Risques de conception à trancher (pas des décisions)

- Valeurs de sévérité non Medium : le brief dit « annotation sinon Medium » ; mapper critical et valeurs inconnues reste à définir (valeurs observées divergentes selon les sources).
- Politique héritée détectée : message dédié plutôt que « échec de l'outil » (à décider).
- Les résultats `warn`, `skip`, `error` ne sont pas des findings Misconfiguration par défaut ; à définir.
- Chercher le fichier : le rapport donne kind, name, namespace, UID ; les manifestes bruts n'ont pas d'UID, donc la clef de rapprochement est kind + namespace + name (collisions possibles entre dossiers ou rendus).

## Non vérifié (à mesurer en exécutant `kyverno apply` 1.20.0)

- Noms et nesting exacts du JSON (`results[].severity`, `results[].resources[]`, `rule`, `policy`, `message`, `category`, `source`).
- Si une ValidatingPolicy sans annotation produit `severity` vide ou absent.
- Sortie JSON quand plusieurs rapports sont émis (lignes JSON successives ?).
- Erreur exacte sur `ClusterPolicy` avec 1.20.0, et code de sortie associé.
- Politique invalide, répertoire de manifestes vide, `generate` et `verifyImages` hors cluster, accès réseau.
- Conftest : prise en charge Helm/Kustomize ; Kubescape : framework personnalisé local sans réseau, numéros de ligne ; Polaris : sorties CLI, licence ; Checkov : champ JSON de ligne ; kube-score : contrôles personnalisés.
- Fréquence d'usage de la politique as code d'organisation (aucune donnée chiffrée récente).

## Sources (consultées le 2026-11-22)

- Kyverno, page `kyverno apply` (référence des flags) : https://kyverno.io/docs/kyverno-cli/reference/kyverno_apply/
- Kyverno, racine de la CLI et usage : https://kyverno.io/docs/kyverno-cli/usage/apply/
- Code, commande apply (codes de sortie, types chargés) : https://raw.githubusercontent.com/kyverno/kyverno/main/cmd/cli/kubectl-kyverno/commands/apply/command.go
- Code, impression des rapports : https://raw.githubusercontent.com/kyverno/kyverno/main/cmd/cli/kubectl-kyverno/commands/apply/print.go
- Code, calcul des rapports : https://raw.githubusercontent.com/kyverno/kyverno/main/cmd/cli/kubectl-kyverno/report/report.go
- Code, chargeur de politiques (rejet des types hérités), tag v1.20.0 : https://raw.githubusercontent.com/kyverno/kyverno/v1.20.0/cmd/cli/kubectl-kyverno/policy/load.go ; main : https://raw.githubusercontent.com/kyverno/kyverno/main/cmd/cli/kubectl-kyverno/policy/load.go
- Versions Kyverno : https://github.com/kyverno/kyverno/releases
- Mise à niveau (1.20, types hérités non exécutés) : https://kyverno.io/docs/installation/upgrading/
- Migration vers CEL : https://kyverno.io/docs/guides/migration-to-cel/
- Rapports de politique : https://kyverno.io/docs/guides/reports
- Image CLI : https://github.com/kyverno/kyverno/pkgs/container/kyverno-cli
- Annotation de sévérité (tiers) : https://docs.defectdojo.com/supported_tools/parsers/file/kyverno/ ; https://docs-bigbang.dso.mil/latest/packages/kyverno-policies/docs/overview/ ; https://main.kyverno.io/docs/policy-types/validating-policy
- Usage CI et pre-commit : https://dev.to/alexandrev/integrating-kyverno-cli-into-cicd-pipelines-with-github-actions-3371 ; https://oneuptime.com/blog/post/2026-01-30-kyverno-cli-usage/view ; https://oneuptime.com/blog/post/2026-03-06-validate-flux-cd-manifests-before-commit-policy-engines/view ; https://dev.to/kcdchennai/how-to-use-kyverno-cli-to-validate-k8s-manifests-3865 ; https://kyverno.io/docs/testing-policies/
- Code de sortie 0 rapporté : https://dev.to/sodiqjimoh/your-kyverno-ci-is-lying-to-you-why-kyverno-cli-exits-0-on-policy-violations-1gg9
- Graduation CNCF : https://www.cncf.io/announcements/2026/03/24/cloud-native-computing-foundation-announces-kyvernos-graduation/
- Enquête sur la gestion des politiques : https://cloudnativenow.com/topics/cloudnativesecurity/the-state-of-policy-management-in-kubernetes/
- Conftest : https://www.conftest.dev/ ; https://www.conftest.dev/install/ ; https://github.com/open-policy-agent/conftest
- Kubescape : https://github.com/kubescape/kubescape ; https://github.com/armosec/regolibrary
- Polaris : https://github.com/FairwindsOps/polaris ; https://polaris.docs.fairwinds.com/customization/custom-checks/
- kube-score : https://github.com/zegl/kube-score
- Checkov : https://github.com/bridgecrewio/checkov ; https://www.checkov.io/2.Basics/Reviewing%20Scan%20Results.html
- Datree (archivé) : https://github.com/datreeio/datree
- Migrations de parc de politiques : https://repo1.dso.mil/groups/big-bang/-/epics/578 ; https://nirmata.com/2026/07/26/kyverno-policy-migration-to-cel-based-policies/
