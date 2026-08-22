// Package registryalias adapte la liste de registries configurée en
// substitutions d'affichage pour `docker.ApplyAliases`.
//
// # Pourquoi un paquet plutôt qu'une fonction dans l'un des deux
//
// `internal/docker` ne connaît pas `internal/config`, et c'est délibéré : c'est
// le pilote de la CLI Docker, et `docker.RegistryAlias` est son type propre pour
// cette raison — un pilote qui apprend le schéma d'un fichier YAML ne peut plus
// être appelé sans lui. L'inverse est pire : `config` décrit ce que
// l'utilisateur écrit, et n'a aucune raison de dépendre de la façon dont Docker
// nomme les choses.
//
// L'adaptateur va donc au-dessus des deux. Il est ici parce que ses deux
// appelants sont des vues — l'onglet Images d'`:oci` et l'inventaire de `:sec`,
// qui affichent la même image et doivent l'écrire pareil.
package registryalias

import (
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
)

// From rend les substitutions déclarées par la configuration.
//
// L'ordre de déclaration est préservé, et ce n'est pas cosmétique :
// `docker.ApplyAliases` retient le **premier** préfixe qui matche, donc c'est
// lui qui départage deux registries dont l'un préfixe l'autre
// (`nexus.example.com` et `nexus.example.com/docker-hosted`). Trier ou
// dédupliquer ici changerait le nom affiché sans que rien ne le dise.
//
// Une entrée sans alias, ou sans URL, ne substitue rien : elle est écartée
// plutôt que portée avec une chaîne vide, qui ferait matcher tout nom d'image.
func From(items []config.RegistryItem) []docker.RegistryAlias {
	aliases := make([]docker.RegistryAlias, 0, len(items))
	for _, item := range items {
		if item.Alias == "" || item.URL == "" {
			continue
		}
		aliases = append(aliases, docker.RegistryAlias{URL: item.URL, Alias: item.Alias})
	}
	return aliases
}
