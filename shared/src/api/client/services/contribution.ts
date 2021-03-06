import { isEqual, mapValues } from 'lodash'
import { BehaviorSubject, combineLatest, isObservable, Observable, of, Subscribable, Unsubscribable, from } from 'rxjs'
import { distinctUntilChanged, map, switchMap } from 'rxjs/operators'
import { combineLatestOrDefault } from '../../../util/rxjs/combineLatestOrDefault'
import { ContributableMenu, Contributions, Evaluated, MenuItemContribution, Raw } from '../../protocol'
import { Context, ContributionScope, getComputedContextProperty } from '../context/context'
import { ComputedContext, Expression, parse, parseTemplate } from '../context/expr/evaluator'
import { EditorService } from './editorService'
import { SettingsService } from './settings'
import { ModelService } from './modelService'

/** A registered set of contributions from an extension in the registry. */
export interface ContributionsEntry {
    /**
     * The contributions, either as a value or an observable.
     *
     * If an observable is used, it should be a cold Observable and emit (e.g., its current value) upon
     * subscription. The {@link ContributionRegistry#contributions} observable blocks until all observables have
     * emitted.
     */
    contributions: Contributions | Observable<Contributions | Contributions[]>
}

/**
 * An unsubscribable that deregisters the contributions it is associated with. It can also be used in
 * ContributionRegistry#replaceContributions.
 */
export interface ContributionUnsubscribable extends Unsubscribable {
    entry: ContributionsEntry
}

/** Manages and executes contributions from all extensions. */
export class ContributionRegistry {
    /** All entries, including entries that are not enabled in the current context. */
    private _entries = new BehaviorSubject<ContributionsEntry[]>([])

    constructor(
        private editorService: Pick<EditorService, 'activeEditorUpdates'>,
        private modelService: Pick<ModelService, 'getPartialModel'>,
        private settingsService: Pick<SettingsService, 'data'>,
        private context: Subscribable<Context<any>>
    ) {}

    /**
     * Register contributions and return an unsubscribable that deregisters the contributions.
     * Any expressions in the contributions need to be already parsed for fast re-evaluation.
     */
    public registerContributions(entry: ContributionsEntry): ContributionUnsubscribable {
        this._entries.next([...this._entries.value, entry])
        return {
            unsubscribe: () => {
                this._entries.next(this._entries.value.filter(e => e !== entry))
            },
            entry,
        }
    }

    /**
     * Atomically deregister the previous contributions and register the next contributions. The registry's observable
     * emits only one time after both operations are complete (instead of also emitting after the deregistration
     * and before the registration).
     */
    public replaceContributions(
        previous: ContributionUnsubscribable,
        next: ContributionsEntry
    ): ContributionUnsubscribable {
        this._entries.next([...this._entries.value.filter(e => e !== previous.entry), next])
        return {
            unsubscribe: () => {
                this._entries.next(this._entries.value.filter(e => e !== next))
            },
            entry: next,
        }
    }

    /**
     * Returns an observable that emits all contributions (merged) evaluated in the current model (with the
     * optional scope). It emits whenever there is any change.
     *
     * @template T Extra allowed property value types for the {@link Context} value. See {@link Context}'s `T` type
     * parameter for more information.
     * @param extraContext Extra context values to use when computing the contributions. Properties in this object
     * shadow (take precedence over) properties in the global context for this computation.
     */
    public getContributions<T>(
        scope?: ContributionScope | undefined,
        extraContext?: Context<T>
    ): Observable<Evaluated<Contributions>> {
        return this.getContributionsFromEntries(this._entries, scope, extraContext)
    }

    /**
     * @template T Extra allowed property value types for the {@link Context} value. See {@link Context}'s `T` type
     * parameter for more information.
     */
    protected getContributionsFromEntries<T>(
        entries: Observable<ContributionsEntry[]>,
        scope: ContributionScope | undefined,
        extraContext?: Context<T>,
        logWarning = (...args: any[]) => console.log(...args)
    ): Observable<Evaluated<Contributions>> {
        return combineLatest([
            entries.pipe(
                switchMap(entries =>
                    combineLatestOrDefault(
                        entries.map(entry =>
                            isObservable<Contributions | Contributions[]>(entry.contributions)
                                ? entry.contributions
                                : of(entry.contributions)
                        ),
                        []
                    )
                )
            ),
            from(this.editorService.activeEditorUpdates).pipe(
                map(activeEditor =>
                    activeEditor
                        ? { ...activeEditor, model: this.modelService.getPartialModel(activeEditor.resource) }
                        : undefined
                )
            ),
            this.settingsService.data,
            this.context,
        ]).pipe(
            map(([multiContributions, activeEditor, settings, context]) => {
                // Merge in extra context.
                if (extraContext) {
                    context = { ...context, ...extraContext }
                }

                // TODO(sqs): use {@link ContextService#observeValue}
                const computedContext = {
                    get: (key: string) => getComputedContextProperty(activeEditor, settings, context, key, scope),
                }
                return multiContributions.flat().map(contributions => {
                    try {
                        return filterContributions(evaluateContributions(computedContext, contributions))
                    } catch (err) {
                        // An error during evaluation causes all of the contributions in the same entry to be
                        // discarded.
                        logWarning('Discarding contributions: evaluating expressions or templates failed.', {
                            contributions,
                            err,
                        })
                        return {}
                    }
                })
            }),
            map(c => mergeContributions(c)),
            distinctUntilChanged((a, b) => isEqual(a, b))
        )
    }

    /**
     * All contribution entries, emitted whenever the set of registered contributions changes.
     *
     * Most callers should use ContributionsRegistry#getContributions. Only use #entries if the
     * caller needs information that is discarded when the contributions are merged (such as the
     * extension that registered each set of contributions).
     */
    public readonly entries: Observable<ContributionsEntry[]> & { value: ContributionsEntry[] } = this._entries
}

/**
 * Merges the contributions.
 *
 * Most callers should use ContributionRegistry#getContributions, which merges all registered
 * contributions.
 */
export function mergeContributions(contributions: Evaluated<Contributions>[]): Evaluated<Contributions> {
    if (contributions.length === 0) {
        return {}
    }
    if (contributions.length === 1) {
        return contributions[0]
    }
    const merged: Evaluated<Contributions> = {}
    for (const c of contributions) {
        if (c.actions) {
            if (!merged.actions) {
                merged.actions = [...c.actions]
            } else {
                merged.actions = [...merged.actions, ...c.actions]
            }
        }
        if (c.menus) {
            if (!merged.menus) {
                merged.menus = { ...c.menus }
            } else {
                for (const [menu, items] of Object.entries(c.menus) as [
                    ContributableMenu,
                    Evaluated<MenuItemContribution>[]
                ][]) {
                    const mergedItems = merged.menus[menu]
                    if (!mergedItems) {
                        merged.menus[menu] = [...items]
                    } else {
                        merged.menus[menu] = [...mergedItems, ...items]
                    }
                }
            }
        }
        if (c.searchFilters) {
            if (!merged.searchFilters) {
                merged.searchFilters = [...c.searchFilters]
            } else {
                merged.searchFilters = [...merged.searchFilters, ...c.searchFilters]
            }
        }
    }
    return merged
}

/**
 * Filters the contributions to only those that are enabled in the current context.
 */
export function filterContributions(contributions: Evaluated<Contributions>): Evaluated<Contributions> {
    if (!contributions.menus) {
        return contributions
    }
    return {
        ...contributions,
        menus: mapValues(contributions.menus, menuItems => menuItems?.filter(menuItem => menuItem.when !== false)),
    }
}

/**
 * Evaluates expressions in contribution definitions against the given context.
 *
 * @todo could walk object recursively
 */
export function evaluateContributions(
    context: ComputedContext,
    contributions: Contributions
): Evaluated<Contributions> {
    return {
        ...contributions,
        menus:
            contributions.menus &&
            mapValues(contributions.menus, (menuItems): Evaluated<MenuItemContribution>[] | undefined =>
                menuItems?.map(menuItem => ({ ...menuItem, when: menuItem.when && !!menuItem.when.exec(context) }))
            ),
        actions: evaluateActionContributions(context, contributions.actions),
    }
}

/**
 * Evaluates expressions in contribution definitions against the given context.
 */
function evaluateActionContributions(
    context: ComputedContext,
    actions: Contributions['actions']
): Evaluated<Contributions['actions']> {
    return actions?.map(action => ({
        ...action,
        title: action.title?.exec(context),
        category: action.category?.exec(context),
        description: action.description?.exec(context),
        iconURL: action.iconURL?.exec(context),
        actionItem: action.actionItem && {
            ...action.actionItem,
            label: action.actionItem.label?.exec(context),
            description: action.actionItem.description?.exec(context),
            iconURL: action.actionItem.iconURL?.exec(context),
            iconDescription: action.actionItem.iconDescription?.exec(context),
            pressed: action.actionItem.pressed?.exec(context),
        },
        commandArguments: action.commandArguments?.map(arg => (arg instanceof Expression ? arg.exec(context) : arg)),
    }))
}

/**
 * Evaluates expressions in contribution definitions against the given context.
 */
export function parseContributionExpressions(contributions: Raw<Contributions>): Contributions {
    return {
        ...contributions,
        menus:
            contributions.menus &&
            mapValues(contributions.menus, (menuItems): MenuItemContribution[] | undefined =>
                menuItems?.map(menuItem => ({
                    ...menuItem,
                    when: typeof menuItem.when === 'string' ? parse<boolean>(menuItem.when) : undefined,
                }))
            ),
        actions: contributions && parseActionContributionExpressions(contributions.actions),
    }
}

const maybe = <T, R>(value: T | undefined, fn: (value: T) => R): R | undefined =>
    value === undefined ? undefined : fn(value)

/**
 * Evaluates expressions in contribution definitions against the given context.
 */
function parseActionContributionExpressions(actions: Raw<Contributions['actions']>): Contributions['actions'] {
    return actions?.map(action => ({
        ...action,
        title: maybe(action.title, parseTemplate),
        category: maybe(action.category, parseTemplate),
        description: maybe(action.description, parseTemplate),
        iconURL: maybe(action.iconURL, parseTemplate),
        actionItem: action.actionItem && {
            ...action.actionItem,
            label: maybe(action.actionItem.label, parseTemplate),
            description: maybe(action.actionItem.description, parseTemplate),
            iconURL: maybe(action.actionItem.iconURL, parseTemplate),
            iconDescription: maybe(action.actionItem.iconDescription, parseTemplate),
            pressed: maybe(action.actionItem.pressed, pressed => parse(pressed)),
        },
        commandArguments: action.commandArguments?.map(arg => (typeof arg === 'string' ? parseTemplate(arg) : arg)),
    }))
}
