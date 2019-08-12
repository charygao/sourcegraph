import MessageOutlineIcon from 'mdi-react/MessageOutlineIcon'
import React from 'react'
import { Link } from 'react-router-dom'
import { LinkOrSpan } from '../../../../../shared/src/components/LinkOrSpan'
import { displayRepoName } from '../../../../../shared/src/components/RepoFileLink'
import * as GQL from '../../../../../shared/src/graphql/schema'
import { ActorLink } from '../../../actor/ActorLink'
import { Timestamp } from '../../../components/time/Timestamp'
import { LabelableLabelsList } from '../../labels/labelable/LabelableLabelsList'
import { ThreadStateIcon } from '../common/threadState/ThreadStateIcon'
import { ThreadListContext } from './ThreadList2'

export interface ThreadListItemContext {
    showRepository?: boolean
}

interface Props extends ThreadListItemContext, ThreadListContext {
    thread: Pick<GQL.ThreadOrThreadPreview, 'title' | 'kind' | 'repository'> &
        (
            | Pick<GQL.IThread, '__typename' | 'id' | 'number' | 'state' | 'url' | 'createdAt' | 'author' | 'comments'>
            | { __typename: 'ThreadPreview'; id?: undefined; number?: undefined; state?: undefined; url?: undefined })

    className?: string
}

/**
 * An item in the list of threads.
 */
export const ThreadListItem: React.FunctionComponent<Props> = ({
    thread,
    showRepository,
    itemCheckboxes,
    className = '',
}) => (
    <li className={`list-group-item ${className}`}>
        <div className="d-flex align-items-start pl-1">
            {itemCheckboxes && (
                <div
                    className="form-check ml-1 mr-2"
                    /* tslint:disable-next-line:jsx-ban-props */
                    style={{ marginTop: '2px' /* stylelint-disable-line declaration-property-unit-whitelist */ }}
                >
                    <input className="form-check-input position-static" type="checkbox" aria-label="Select item" />
                </div>
            )}
            <ThreadStateIcon
                thread={thread.state ? thread : { kind: thread.kind, state: GQL.ThreadState.OPEN }}
                className="mr-2"
            />
            <div className="flex-1">
                <div className="d-flex align-items-center flex-wrap">
                    <h3 className="d-flex align-items-center mb-0 mr-2">
                        <LinkOrSpan to={thread.url} className="text-decoration-none text-body">
                            {thread.title}
                        </LinkOrSpan>
                        <span className="badge badge-secondary ml-1 d-none">123</span> {/* TODO!(sqs) */}
                    </h3>
                    {thread.__typename === 'Thread' && (
                        <LabelableLabelsList labelable={thread} showNoLabels={false} itemClassName="mr-2" />
                    )}
                </div>
                <ul className="list-unstyled d-flex align-items-center small text-muted mb-0">
                    <li>
                        {thread.number !== undefined ? (
                            <span className="text-muted mr-2">
                                {showRepository && (
                                    <Link to={thread.repository.url} className="text-muted">
                                        {displayRepoName(thread.repository.name)}
                                    </Link>
                                )}
                                #{thread.number}
                            </span>
                        ) : (
                            <span className="text-muted mr-2">
                                New {thread.kind.toLowerCase()} in{' '}
                                <Link to={thread.repository.url}>{displayRepoName(thread.repository.name)}</Link>:
                            </span>
                        )}
                    </li>
                    {thread.__typename === 'Thread' && (
                        <li>
                            created <Timestamp date={thread.createdAt} /> by <ActorLink actor={thread.author} />
                        </li>
                    )}
                    {/* TODO!(sqs): show contents {thread.targets.totalCount > 0 && (
                        <li className="ml-2 d-flex align-items-center">
                            <FileFindIcon className="icon-inline mr-1" /> {thread.targets.totalCount}{' '}
                            {pluralize('item', thread.targets.totalCount)}
                        </li>
                    )}*/}
                </ul>
            </div>
            <div>
                <ul className="list-inline d-flex align-items-center">
                    {thread.__typename === 'Thread' && thread.comments.totalCount > 0 && (
                        <li className="list-inline-item">
                            <small className="text-muted">
                                <MessageOutlineIcon className="icon-inline" /> {thread.comments.totalCount}
                            </small>
                        </li>
                    )}
                </ul>
            </div>
        </div>
    </li>
)
