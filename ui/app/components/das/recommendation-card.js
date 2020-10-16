import Component from '@glimmer/component';
import { tracked } from '@glimmer/tracking';
import { action } from '@ember/object';
import ResourcesDiffs from 'nomad-ui/utils/resources-diffs';
import { htmlSafe } from '@ember/template';

export default class DasRecommendationCardComponent extends Component {
  @tracked allCpuToggleActive = true;
  @tracked allMemoryToggleActive = true;

  @tracked activeTaskToggleRowIndex = 0;

  get activeTaskToggleRow() {
    return this.taskToggleRows[this.activeTaskToggleRowIndex];
  }

  get activeTask() {
    return this.activeTaskToggleRow.task;
  }

  get narrative() {
    const summary = this.args.summary;
    const taskGroup = summary.taskGroup;

    const diffs = new ResourcesDiffs(
      taskGroup,
      taskGroup.count,
      this.args.summary.recommendations,
      this.args.summary.excludedRecommendations
    );

    const cpuDelta = diffs.cpu.delta;
    const memoryDelta = diffs.memory.delta;

    const aggregate = taskGroup.count > 1;
    const aggregateString = aggregate ? ' an aggregate' : '';

    if (cpuDelta || memoryDelta) {
      const deltasSameDirection = cpuDelta < 0 && memoryDelta < 0 || cpuDelta > 0 && memoryDelta > 0;

      let narrative = 'Applying the selected recommendations will';
      
      if (deltasSameDirection) {
        narrative += ` ${verbForDelta(cpuDelta)} ${aggregateString}`;
      }

      if (cpuDelta) {
        if (!deltasSameDirection) {
          narrative += ` ${verbForDelta(cpuDelta)} ${aggregateString}`;
        }

        narrative += ` <strong>${diffs.cpu.absoluteAggregateDiff} of CPU</strong>`;
      }

      if (cpuDelta && memoryDelta) {
        narrative += ' and';
      }

      if (memoryDelta) {
        if (!deltasSameDirection) {
          narrative += ` ${verbForDelta(memoryDelta)} ${aggregateString}`;
        }

        narrative += ` <strong>${diffs.memory.absoluteAggregateDiff} of memory</strong>`;
      }

      if (taskGroup.count === 1) {
        narrative += '.';
      } else {
        narrative += ` across <strong>${taskGroup.count} allocations</strong>.`;
      }

      return htmlSafe(narrative);
    } else {
      return '';
    }
  }

  get taskToggleRows() {
    const taskNameToTaskToggles = {};

    return this.args.summary.recommendations.reduce((taskToggleRows, recommendation) => {
      let taskToggleRow = taskNameToTaskToggles[recommendation.task.name];

      if (!taskToggleRow) {
        taskToggleRow = {
          recommendations: [],
          task: recommendation.task,
        };

        taskNameToTaskToggles[recommendation.task.name] = taskToggleRow;
        taskToggleRows.push(taskToggleRow);
      }

      const rowResourceProperty = recommendation.resource === 'CPU' ? 'cpu' : 'memory';

      taskToggleRow[rowResourceProperty] = {
        recommendation,
        isActive: !this.args.summary.excludedRecommendations.includes(recommendation),
      };

      taskToggleRow.recommendations.push(recommendation);

      return taskToggleRows;
    }, []);
  }

  get showToggleAllToggles() {
    return this.taskToggleRows.length > 1;
  }

  get allCpuToggleDisabled() {
    return !this.args.summary.recommendations.filterBy('resource', 'CPU').length;
  }

  get allMemoryToggleDisabled() {
    return !this.args.summary.recommendations.filterBy('resource', 'MemoryMB').length;
  }

  get cannotAccept() {
    return (
      this.args.summary.excludedRecommendations.length == this.args.summary.recommendations.length
    );
  }

  @action
  toggleAllRecommendationsForResource(resource) {
    let enabled;

    if (resource === 'CPU') {
      this.allCpuToggleActive = !this.allCpuToggleActive;
      enabled = this.allCpuToggleActive;
    } else {
      this.allMemoryToggleActive = !this.allMemoryToggleActive;
      enabled = this.allMemoryToggleActive;
    }

    this.args.summary.toggleAllRecommendationsForResource(resource, enabled);
  }

  @action
  accept() {
    this.args.summary.save().then(this.args.onProcessed, this.args.onError);
  }

  @action
  dismiss() {
    this.args.summary.excludedRecommendations.pushObjects(this.args.summary.recommendations);
    this.args.summary.save().then(this.args.onDismissed, this.args.onError);
  }
}

function verbForDelta(delta) {
  if (delta > 0) {
    return 'add';
  } else {
    return 'save';
  }
}