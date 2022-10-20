import { Breadcrumb, Card, Tabs } from 'antd';
import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useLocation, useNavigate, useParams } from 'react-router-dom';

import InfoBox from 'components/InfoBox';
import Link from 'components/Link';
import MetadataCard from 'components/Metadata/MetadataCard';
import NotesCard from 'components/NotesCard';
import Page from 'components/Page';
import PageNotFound from 'components/PageNotFound';
import { paths } from 'routes/utils';
import { getModelVersion, patchModelVersion } from 'services/api';
import Message, { MessageType } from 'shared/components/Message';
import Spinner from 'shared/components/Spinner/Spinner';
import usePolling from 'shared/hooks/usePolling';
import { isEqual } from 'shared/utils/data';
import { ErrorType } from 'shared/utils/error';
import { isAborted, isNotFound } from 'shared/utils/service';
import { humanReadableBytes } from 'shared/utils/string';
import { ModelVersion } from 'types';
import handleError from 'utils/error';
import { checkpointSize } from 'utils/workload';

import css from './ModelVersionDetails.module.scss';
import ModelVersionHeader from './ModelVersionDetails/ModelVersionHeader';

const { TabPane } = Tabs;

const TabType = {
  Model: 'model',
  Notes: 'notes',
} as const;

type Params = {
  modelId: string;
  tab?: typeof TabType[keyof typeof TabType];
  versionId: string;
};

const TAB_KEYS = Object.values(TabType);
const DEFAULT_TAB_KEY = TabType.Model;

const ModelVersionDetails: React.FC = () => {
  const [modelVersion, setModelVersion] = useState<ModelVersion>();
  const { modelId: modelID, versionId: versionID, tab } = useParams<Params>();
  const [pageError, setPageError] = useState<Error>();
  const navigate = useNavigate();
  const location = useLocation();
  const [tabKey, setTabKey] = useState(tab && TAB_KEYS.includes(tab) ? tab : DEFAULT_TAB_KEY);

  const modelId = modelID ?? '';
  const versionId = versionID ?? '';

  const basePath = paths.modelVersionDetails(modelId, versionId);

  const fetchModelVersion = useCallback(async () => {
    try {
      const versionData = await getModelVersion({
        modelName: modelId,
        versionId: parseInt(versionId),
      });
      /**
       * TODO: can this compare againt prev instead of modelVersion, so that
       * modelVersion can be remove from deps? would need to get modelVersion
       * out of deps in order to repoll on change fn
       */
      setModelVersion((prev) => (!isEqual(versionData, modelVersion) ? versionData : prev));
    } catch (e) {
      if (!pageError && !isAborted(e)) setPageError(e as Error);
    }
  }, [modelId, modelVersion, pageError, versionId]);

  usePolling(fetchModelVersion);

  const handleTabChange = useCallback(
    (key) => {
      navigate(`${basePath}/${key}`, { replace: true });
    },
    [basePath, navigate],
  );

  useEffect(() => {
    setTabKey(tab ?? DEFAULT_TAB_KEY);
  }, [location.pathname, tab]);

  // Sets the default sub route.
  useEffect(() => {
    if (!tab || (tab && !TAB_KEYS.includes(tab))) {
      if (window.location.pathname.includes(basePath))
        navigate(`${basePath}/${tabKey}`, { replace: true });
    }
  }, [basePath, navigate, tab, tabKey]);

  const saveMetadata = useCallback(
    async (editedMetadata) => {
      try {
        await patchModelVersion({
          body: { metadata: editedMetadata, modelName: modelId },
          modelName: modelId,
          versionId: parseInt(versionId),
        });
        await fetchModelVersion();
      } catch (e) {
        handleError(e, {
          publicSubject: 'Unable to save metadata.',
          silent: false,
          type: ErrorType.Api,
        });
      }
    },
    [fetchModelVersion, modelId, versionId],
  );

  const saveNotes = useCallback(
    async (editedNotes: string) => {
      try {
        const versionResponse = await patchModelVersion({
          body: { modelName: modelId, notes: editedNotes },
          modelName: modelId,
          versionId: parseInt(versionId),
        });
        setModelVersion(versionResponse);
      } catch (e) {
        handleError(e, {
          publicSubject: 'Unable to update notes.',
          silent: true,
          type: ErrorType.Api,
        });
      }
    },
    [modelId, versionId],
  );

  const saveDescription = useCallback(
    async (editedDescription: string) => {
      try {
        await patchModelVersion({
          body: { comment: editedDescription, modelName: modelId },
          modelName: modelId,
          versionId: parseInt(versionId),
        });
      } catch (e) {
        handleError(e, {
          publicSubject: 'Unable to save description.',
          silent: false,
          type: ErrorType.Api,
        });
      }
    },
    [modelId, versionId],
  );

  const saveName = useCallback(
    async (editedName: string) => {
      try {
        await patchModelVersion({
          body: { modelName: modelId, name: editedName },
          modelName: modelId,
          versionId: parseInt(versionId),
        });
      } catch (e) {
        handleError(e, {
          publicSubject: 'Unable to save name.',
          silent: false,
          type: ErrorType.Api,
        });
      }
    },
    [modelId, versionId],
  );

  const saveVersionTags = useCallback(
    async (newTags) => {
      try {
        await patchModelVersion({
          body: { labels: newTags, modelName: modelId },
          modelName: modelId,
          versionId: parseInt(versionId),
        });
        fetchModelVersion();
      } catch (e) {
        handleError(e, {
          publicSubject: 'Unable to save tags.',
          silent: false,
          type: ErrorType.Api,
        });
      }
    },
    [fetchModelVersion, modelId, versionId],
  );

  const renderResource = (resource: string, size: string): React.ReactNode => {
    return (
      <div className={css.resource} key={resource}>
        <div className={css.resourceName}>{resource}</div>
        <div className={css.resourceSpacer} />
        <div className={css.resourceSize}>{size}</div>
      </div>
    );
  };

  const checkpointInfo = useMemo(() => {
    if (!modelVersion?.checkpoint) return [];
    const checkpointResources = modelVersion.checkpoint.resources || {};
    const resources = Object.keys(modelVersion.checkpoint.resources || {})
      .sort((a, b) => checkpointResources[a] - checkpointResources[b])
      .map((key) => ({ name: key, size: humanReadableBytes(checkpointResources[key]) }));
    const hasExperiment = !!modelVersion.checkpoint.experimentId;
    return [
      {
        content: hasExperiment ? (
          <Breadcrumb className={css.link}>
            <Breadcrumb.Item>
              <Link path={paths.experimentDetails(modelVersion.checkpoint.experimentId || '')}>
                Experiment {modelVersion.checkpoint.experimentId}
              </Link>
            </Breadcrumb.Item>
            {!!modelVersion.checkpoint.trialId && (
              <Breadcrumb.Item>
                <Link
                  path={paths.trialDetails(
                    modelVersion.checkpoint.trialId,
                    modelVersion.checkpoint.experimentId,
                  )}>
                  Trial {modelVersion.checkpoint.trialId}
                </Link>
              </Breadcrumb.Item>
            )}
            {!!modelVersion.checkpoint.totalBatches && (
              <Breadcrumb.Item>Batch {modelVersion.checkpoint.totalBatches}</Breadcrumb.Item>
            )}
          </Breadcrumb>
        ) : (
          <Breadcrumb>
            <Breadcrumb.Item>Task {modelVersion.checkpoint.taskId}</Breadcrumb.Item>
          </Breadcrumb>
        ),
        label: 'Source',
      },
      { content: modelVersion.checkpoint.uuid, label: 'Checkpoint UUID' },
      {
        content: humanReadableBytes(checkpointSize(modelVersion.checkpoint)),
        label: 'Total Size',
      },
      {
        content: resources.map((resource) => renderResource(resource.name, resource.size)),
        label: 'Code',
      },
    ];
  }, [modelVersion?.checkpoint]);

  const validationMetrics = useMemo(() => {
    if (!modelVersion?.checkpoint) return [];
    const metrics = Object.entries(modelVersion?.checkpoint?.validationMetrics?.avgMetrics || {});
    return metrics.map((metric) => ({
      content: metric[1],
      label: metric[0],
    }));
  }, [modelVersion?.checkpoint]);

  if (!modelId) {
    return <Message title="Model name is empty" />;
  } else if (isNaN(parseInt(versionId))) {
    return <Message title={`Invalid Version ID ${versionId}`} />;
  } else if (pageError) {
    if (isNotFound(pageError)) return <PageNotFound />;
    const message = `Unable to fetch model ${modelId} version ${versionId}`;
    return <Message title={message} type={MessageType.Warning} />;
  } else if (!modelVersion) {
    return <Spinner tip={`Loading model ${modelId} version ${versionId} details...`} />;
  }

  return (
    <Page
      bodyNoPadding
      docTitle="Model Version Details"
      headerComponent={
        <ModelVersionHeader
          modelVersion={modelVersion}
          onSaveDescription={saveDescription}
          onSaveName={saveName}
          onUpdateTags={saveVersionTags}
        />
      }
      id="modelDetails">
      <Tabs
        activeKey={tabKey}
        style={{ height: 'auto' }}
        tabBarStyle={{ backgroundColor: 'var(--theme-colors-monochrome-17)', paddingLeft: 24 }}
        onChange={handleTabChange}>
        <TabPane key={TabType.Model} tab="Model">
          <div className={css.base}>
            <Card title="Model Checkpoint">
              <InfoBox rows={checkpointInfo} separator />
            </Card>
            <Card title="Validation Metrics">
              <InfoBox rows={validationMetrics} separator />
            </Card>
            <MetadataCard
              disabled={modelVersion.model.archived}
              metadata={modelVersion.metadata}
              onSave={saveMetadata}
            />
          </div>
        </TabPane>
        <TabPane key={TabType.Notes} tab="Notes">
          <div className={css.base}>
            <NotesCard
              disabled={modelVersion.model.archived}
              notes={modelVersion.notes ?? ''}
              onSave={saveNotes}
            />
          </div>
        </TabPane>
      </Tabs>
    </Page>
  );
};

export default ModelVersionDetails;
